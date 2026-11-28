// Cross-file validation pass, resolves the `sourceDescriptions[].url`
// references, loads each OpenAPI document from disk via the
// oasresolver package, and checks that every step's operation
// reference (`operationId` or `operationPath`) points at an operation
// that actually exists in the right source.
//
// HTTPS URLs are fetched only when remote loading is explicitly allowed:
// by default lint stays offline and deterministic (eco-design rule).

package linter

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
)

// operationIndex maps a source name → its loaded OpenAPI source.
type operationIndex map[string]*oasresolver.Source

func lintCrossFile(doc *model.ArazzoDocument, basePath string, remote oasresolver.RemoteOptions) []Issue {
	if doc == nil || basePath == "" {
		return nil
	}
	index, issues := buildOperationIndex(doc, basePath, remote)
	// When a single source is declared and a step uses the short
	// `operationId: foo` form, we resolve `foo` against this source.
	var implicitSource string
	if len(doc.SourceDescriptions) == 1 {
		implicitSource = doc.SourceDescriptions[0].Name
	}
	multipleSources := len(doc.SourceDescriptions) > 1
	// Declared type per source: operation references must land on an
	// openapi source, and LoadAll skips the other types without a
	// SourceResult, so absence from the index is only benign (already
	// reported as a loading error) for openapi sources.
	sourceTypes := make(map[string]string, len(doc.SourceDescriptions))
	for _, s := range doc.SourceDescriptions {
		sourceTypes[s.Name] = s.Type
	}
	for _, wf := range doc.Workflows {
		for _, step := range wf.Steps {
			issues = append(issues, checkStepOperation(step, &wf, index, implicitSource, multipleSources, sourceTypes)...)
			issues = append(issues, checkStepOperationPath(step, &wf, index, sourceTypes)...)
			issues = append(issues, checkStepChannelPath(step, &wf, sourceTypes)...)
		}
	}
	return issues
}

// nonOpenAPISource returns the declared type when it prevents operation
// resolution (anything other than openapi, whose absence from the index
// means a loading error that was already reported).
func nonOpenAPISource(sourceTypes map[string]string, name string) (string, bool) {
	typ, declared := sourceTypes[name]
	if !declared || typ == "" || typ == "openapi" {
		return "", false
	}
	return typ, true
}

func buildOperationIndex(doc *model.ArazzoDocument, basePath string, remote oasresolver.RemoteOptions) (operationIndex, []Issue) {
	index := make(operationIndex, len(doc.SourceDescriptions))
	var issues []Issue
	for _, r := range oasresolver.LoadAll(doc.SourceDescriptions, basePath, remote) {
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
	issues = append(issues, checkAsyncAPIFiles(doc.SourceDescriptions, basePath)...)
	return index, issues
}

// checkAsyncAPIFiles reports local asyncapi sources whose file is
// missing. Their content is not parsed, and remote ones are not fetched.
func checkAsyncAPIFiles(descs []model.SourceDescription, basePath string) []Issue {
	var issues []Issue
	for _, src := range descs {
		if src.Type != "asyncapi" {
			continue
		}
		if u, err := url.Parse(src.URL); err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) {
			continue
		}
		r := oasresolver.SourceResult{Name: src.Name, URL: src.URL}
		r.Path, r.Err = oasresolver.ResolveURL(src.URL, basePath)
		if r.Err == nil {
			_, r.Err = os.Stat(r.Path)
		}
		if r.Err != nil {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     "sourceDescriptions[" + src.Name + "]",
				Message:  sourceErrMessage(r),
			})
		}
	}
	return issues
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

// asyncNotResolved is the warning for an operation reference into an
// asyncapi source: the reference is accepted, but nothing checks that
// the AsyncAPI document defines it.
func asyncNotResolved(path, target, sourceName string) Issue {
	return Issue{
		Severity: SeverityWarning,
		Path:     path,
		Message:  fmt.Sprintf("%q not checked: lint does not resolve AsyncAPI documents (source %q)", target, sourceName),
	}
}

// checkStepChannelPath validates a step's `channelPath`: the canonical
// '{$sourceDescriptions.<name>.url}#/channels/<channel>' form, naming
// a declared asyncapi source. The channel itself is not resolved.
func checkStepChannelPath(step model.Step, wf *model.Workflow, sourceTypes map[string]string) []Issue {
	if step.ChannelPath == "" {
		return nil
	}
	path := fmt.Sprintf("workflows[%s].steps[%s].channelPath", wf.WorkflowID, step.StepID)
	sourceName, channel, ok := oasresolver.ChannelPathTarget(step.ChannelPath)
	if !ok {
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message: fmt.Sprintf(
				"malformed channelPath %q\n\thint: expected {$sourceDescriptions.<name>.url}#/channels/<channel>",
				step.ChannelPath,
			),
		}}
	}
	typ, declared := sourceTypes[sourceName]
	if !declared {
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message:  fmt.Sprintf("channelPath references source description %q which does not exist", sourceName),
		}}
	}
	if typ != "asyncapi" {
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message:  fmt.Sprintf("channelPath references source %q of type %q; channels live in an asyncapi source", sourceName, typeOrDefault(typ)),
		}}
	}
	return []Issue{asyncNotResolved(path, "channel "+channel, sourceName)}
}

// typeOrDefault returns a source description type, an omitted one
// reading as openapi like everywhere else in the linter.
func typeOrDefault(typ string) string {
	if typ == "" {
		return "openapi"
	}
	return typ
}

// checkStepOperation validates a single step's `operationId` against
// the operation index. `implicitSource` is the source name a short-form
// (unqualified) operationId resolves to, empty when no single source
// can disambiguate (either zero or multiple declared).
func checkStepOperation(step model.Step, wf *model.Workflow, index operationIndex, implicitSource string, multipleSources bool, sourceTypes map[string]string) []Issue {
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
	if _, declared := sourceTypes[sourceName]; !declared {
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message:  fmt.Sprintf("operationId references source description %q which does not exist", sourceName),
		}}
	}
	if typ, isOther := nonOpenAPISource(sourceTypes, sourceName); isOther {
		if typ == "asyncapi" {
			return []Issue{asyncNotResolved(path, opID, sourceName)}
		}
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message:  fmt.Sprintf("operationId references source %q of type %q; operations can only be resolved against an openapi source", sourceName, typ),
		}}
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
func checkStepOperationPath(step model.Step, wf *model.Workflow, index operationIndex, sourceTypes map[string]string) []Issue {
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
	if _, declared := sourceTypes[sourceName]; !declared {
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message:  fmt.Sprintf("operationPath references source description %q which does not exist", sourceName),
		}}
	}
	if typ, isOther := nonOpenAPISource(sourceTypes, sourceName); isOther {
		if typ == "asyncapi" {
			return []Issue{asyncNotResolved(path, pointer, sourceName)}
		}
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message:  fmt.Sprintf("operationPath references source %q of type %q; operations can only be resolved against an openapi source", sourceName, typ),
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
