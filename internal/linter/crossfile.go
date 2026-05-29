// Cross-file validation pass, resolves the `sourceDescriptions[].url`
// references, loads each OpenAPI document from disk via the
// oasresolver package, and checks that every step's `operationId`
// points at an operation that actually exists in the right source.
//
// HTTP/HTTPS URLs are intentionally rejected (eco-design rule: lint
// must stay offline and deterministic). HTTPS support would require
// caching, TLS handling and is out of scope for v1.

package linter

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
)

// operationIndex maps a source name → its loaded OpenAPI source.
type operationIndex map[string]*oasresolver.Source

func lintCrossFile(doc *model.ArazzoDocument, basePath string) []Issue {
	if doc == nil || basePath == "" {
		return nil
	}
	index, issues := buildOperationIndex(doc, basePath)
	if len(index) == 0 {
		// Nothing to check against, still surface any source-loading errors.
		return issues
	}
	// When a single source is declared and a step uses the short
	// `operationId: foo` form, we resolve `foo` against this source.
	var implicitSource string
	if len(doc.SourceDescriptions) == 1 {
		implicitSource = doc.SourceDescriptions[0].Name
	}
	multipleSources := len(doc.SourceDescriptions) > 1
	for _, wf := range doc.Workflows {
		for _, step := range wf.Steps {
			issues = append(issues, checkStepOperation(step, &wf, index, implicitSource, multipleSources)...)
		}
	}
	return issues
}

func buildOperationIndex(doc *model.ArazzoDocument, basePath string) (operationIndex, []Issue) {
	index := make(operationIndex, len(doc.SourceDescriptions))
	var issues []Issue
	for _, src := range doc.SourceDescriptions {
		// Only `openapi` sources can be resolved today. Other types
		// (`arazzo` for nested workflows) are out of scope for the
		// first cross-file iteration.
		if src.Type != "" && src.Type != "openapi" {
			continue
		}
		path, err := resolveSourceURL(src.URL, basePath)
		if err != nil {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     "sourceDescriptions[" + src.Name + "]",
				Message:  err.Error(),
			})
			continue
		}
		loaded, err := oasresolver.Load(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				issues = append(issues, Issue{
					Severity: SeverityError,
					Path:     "sourceDescriptions[" + src.Name + "]",
					Message: fmt.Sprintf(
						"file not found\n\turl: %s\n\tresolved to: %s",
						src.URL, path,
					),
				})
			} else {
				issues = append(issues, Issue{
					Severity: SeverityError,
					Path:     "sourceDescriptions[" + src.Name + "]",
					Message:  fmt.Sprintf("cannot load %s: %s", path, err),
				})
			}
			continue
		}
		index[src.Name] = loaded
	}
	return index, issues
}

// resolveSourceURL turns the YAML `url:` field into an absolute local
// path, or returns an error if the URL is unsupported (HTTP/HTTPS).
func resolveSourceURL(rawURL, basePath string) (string, error) {
	if rawURL == "" {
		return "", errors.New("missing url")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return "", fmt.Errorf(
			"HTTP source URLs are not supported\n\turl: %s\n\thint: use a local file or a relative path",
			rawURL,
		)
	case "file":
		return u.Path, nil
	case "":
		if filepath.IsAbs(rawURL) {
			return rawURL, nil
		}
		return filepath.Join(basePath, rawURL), nil
	}
	return "", fmt.Errorf("unsupported url scheme %q", u.Scheme)
}

// checkStepOperation validates a single step's `operationId` against
// the operation index. `implicitSource` is the source name a short-form
// (unqualified) operationId resolves to, empty when no single source
// can disambiguate (either zero or multiple declared).
func checkStepOperation(step model.Step, wf *model.Workflow, index operationIndex, implicitSource string, multipleSources bool) []Issue {
	if step.OperationID == "" {
		return nil
	}
	path := fmt.Sprintf("workflows[%s].steps[%s].operationId", wf.WorkflowID, step.StepID)
	sourceName, opID, qualified := parseOperationRef(step.OperationID)

	if !qualified {
		if multipleSources {
			return []Issue{{
				Severity: SeverityError,
				Path:     path,
				Message: fmt.Sprintf(
					"unqualified operationId %q but multiple sourceDescriptions are declared\n\thint: use $sourceDescriptions.<name>.%s to disambiguate",
					opID, opID,
				),
			}}
		}
		sourceName = implicitSource
	}
	if sourceName == "" {
		// No source declared at all, the JSON Schema pass already
		// reported sourceDescriptions as missing/empty.
		return nil
	}
	src, ok := index[sourceName]
	if !ok {
		// The source is declared but couldn't be loaded, the
		// source-loading error was already reported by
		// buildOperationIndex; don't pile on with a confusing
		// secondary message.
		return nil
	}
	if !src.HasOperationID(opID) {
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message:  fmt.Sprintf("operation %q not found in source %q", opID, sourceName),
		}}
	}
	return nil
}

// parseOperationRef recognises the two accepted forms of operationId:
//
//	"createOrder"                            → short form (qualified=false)
//	"$sourceDescriptions.shop-api.createOrder" → qualified form
func parseOperationRef(ref string) (source, opID string, qualified bool) {
	const prefix = "$sourceDescriptions."
	if !strings.HasPrefix(ref, prefix) {
		return "", ref, false
	}
	rest := strings.TrimPrefix(ref, prefix)
	idx := strings.Index(rest, ".")
	if idx < 0 {
		return rest, "", true
	}
	return rest[:idx], rest[idx+1:], true
}
