// Cross-file validation pass, resolves the `sourceDescriptions[].url`
// references, loads each OpenAPI document from disk via the
// oasresolver package, and checks that every step's operation
// reference (`operationId` or `operationPath`) points at an operation
// that actually exists in the right source.
//
// HTTP/HTTPS URLs are intentionally rejected (eco-design rule: lint
// must stay offline and deterministic). HTTPS support would require
// caching, TLS handling and is out of scope for v1.

package linter

import (
	"errors"
	"fmt"
	"io/fs"
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
	declared := make(map[string]bool, len(doc.SourceDescriptions))
	for _, s := range doc.SourceDescriptions {
		declared[s.Name] = true
	}
	for _, wf := range doc.Workflows {
		for _, step := range wf.Steps {
			issues = append(issues, checkStepOperation(step, &wf, index, implicitSource, multipleSources)...)
			issues = append(issues, checkStepOperationPath(step, &wf, index, declared)...)
		}
	}
	return issues
}

func buildOperationIndex(doc *model.ArazzoDocument, basePath string) (operationIndex, []Issue) {
	index := make(operationIndex, len(doc.SourceDescriptions))
	var issues []Issue
	for _, r := range oasresolver.LoadAll(doc.SourceDescriptions, basePath) {
		if r.Err != nil {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     "sourceDescriptions[" + r.Name + "]",
				Message:  sourceErrMessage(r),
			})
			continue
		}
		index[r.Name] = r.Source
	}
	return index, issues
}

func sourceErrMessage(r oasresolver.SourceResult) string {
	if errors.Is(r.Err, fs.ErrNotExist) {
		return fmt.Sprintf("file not found\n\turl: %s\n\tresolved to: %s", r.URL, r.Path)
	}
	if r.Path != "" {
		return fmt.Sprintf("cannot load %s: %s", r.Path, r.Err)
	}
	return r.Err.Error()
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

// checkStepOperationPath validates a step's `operationPath`: the value
// must be the canonical '{$sourceDescriptions.<name>.url}#<pointer>'
// form, name a declared source, and its JSON pointer must address an
// operation that exists in that source.
func checkStepOperationPath(step model.Step, wf *model.Workflow, index operationIndex, declared map[string]bool) []Issue {
	if step.OperationPath == "" {
		return nil
	}
	path := fmt.Sprintf("workflows[%s].steps[%s].operationPath", wf.WorkflowID, step.StepID)
	sourceName, pointer, ok := oasresolver.SplitOperationPath(step.OperationPath)
	if !ok {
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message: fmt.Sprintf(
				"malformed operationPath %q\n\thint: expected {$sourceDescriptions.<name>.url}#/paths/<escaped-path>/<method>",
				step.OperationPath,
			),
		}}
	}
	if !declared[sourceName] {
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message:  fmt.Sprintf("operationPath references source description %q which does not exist", sourceName),
		}}
	}
	src, loaded := index[sourceName]
	if !loaded {
		// The source is declared but couldn't be loaded; the loading
		// error was already reported by buildOperationIndex.
		return nil
	}
	method, opPath, isOp := oasresolver.OperationPointerTarget(pointer)
	if !isOp {
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message: fmt.Sprintf(
				"operationPath pointer %q does not address an operation\n\thint: expected #/paths/<escaped-path>/<method>",
				pointer,
			),
		}}
	}
	if _, err := src.ResolveOperationPointer(pointer); err != nil {
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message:  fmt.Sprintf("no %s operation on path %q in source %q", method, opPath, sourceName),
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
