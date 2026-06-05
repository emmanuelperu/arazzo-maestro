// Package hurlgen renders an Arazzo workflow as a Hurl (.hurl) e2e
// test file.
//
// Mapping:
//
//	step                      -> one HTTP request block
//	step.outputs              -> [Captures]
//	step.successCriteria      -> comments inside [Asserts]
//	                             (the Arazzo condition mini-language
//	                             is not translated to Hurl predicates)
//	parameters in=header      -> header lines
//	parameters in=query       -> [QueryStringParams]
//	parameters in=path        -> substituted into the request URL
//
// Operation resolution goes through the oasresolver package: callers
// pass a map of source-description name to *oasresolver.Source. Short
// operationId forms ("listUsers") resolve when exactly one source is
// configured; the qualified form ("$sourceDescriptions.<name>.<id>")
// works with any number of sources. Steps whose operationId cannot be
// resolved emit a placeholder request line and a comment naming the
// unresolved id, so the output stays valid Hurl that a human can
// patch.
//
// Arazzo runtime expressions are translated: $inputs.foo becomes
// {{foo}}, $steps.s.outputs.o becomes {{s_o}}, $response.body#/x/y
// becomes `jsonpath "$.x.y"`. Unknown forms pass through unchanged.
//
// The request host is never hard-coded: every request line is prefixed
// with the {{baseUrl}} Hurl variable, so the same .hurl file can run
// against any environment by passing `hurl --variable baseUrl=<endpoint>`.
// The OpenAPI `servers:` URL, when present, is surfaced in the header as
// the documented default rather than baked into the requests.
package hurlgen

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
)

// Generate renders the workflow as a Hurl test file.
//
// sources is keyed by Arazzo sourceDescription name; the caller is
// expected to have loaded each one via oasresolver.Load. A nil or
// empty map is accepted, in which case every step is rendered with an
// unresolved placeholder.
func Generate(wf model.Workflow, sources map[string]*oasresolver.Source) (string, error) {
	var b strings.Builder
	writeHeader(&b, wf, defaultBaseURL(wf, sources))
	unquoted := nonStringInputs(wf.Inputs)
	for i, step := range wf.Steps {
		if i > 0 {
			b.WriteString("\n")
		}
		writeStep(&b, step, sources, unquoted)
	}
	return b.String(), nil
}

// nonStringInputs lists the inputs whose body templates are emitted
// without quotes so the substituted value keeps its JSON type.
func nonStringInputs(inputs []model.InputProperty) map[string]bool {
	m := make(map[string]bool)
	for _, in := range inputs {
		switch in.Type {
		case "number", "integer", "boolean":
			m[in.Name] = true
		}
	}
	return m
}

func writeHeader(b *strings.Builder, wf model.Workflow, defaultBase string) {
	fmt.Fprintf(b, "# Workflow: %s\n", wf.WorkflowID)
	if wf.Summary != "" {
		fmt.Fprintf(b, "# %s\n", wf.Summary)
	}
	b.WriteString("#\n# Base URL (required): pass via `hurl --variable baseUrl=<endpoint>`\n")
	if defaultBase != "" {
		fmt.Fprintf(b, "#   default (OpenAPI servers): %s\n", defaultBase)
	}
	if len(wf.Inputs) > 0 {
		b.WriteString("#\n# Inputs (pass via `hurl --variable name=value`):\n")
		for _, in := range wf.Inputs {
			fmt.Fprintf(b, "#   - %s (%s)\n", in.Name, in.Type)
		}
	}
	b.WriteString("\n")
}

// defaultBaseURL returns the OpenAPI `servers:` URL backing the
// workflow's first resolvable step, or "" when no step resolves or none
// of the sources declares a server. It is documentation only: requests
// always use the {{baseUrl}} variable so the value can be overridden per
// environment at run time.
func defaultBaseURL(wf model.Workflow, sources map[string]*oasresolver.Source) string {
	for _, s := range wf.Steps {
		srcName, opID := parseOpRef(s.OperationID, sources)
		if srcName == "" {
			continue
		}
		src, ok := sources[srcName]
		if !ok {
			continue
		}
		op, err := src.Resolve(opID)
		if err != nil {
			continue
		}
		if op.BaseURL != "" {
			return op.BaseURL
		}
	}
	return ""
}

func writeStep(b *strings.Builder, s model.Step, sources map[string]*oasresolver.Source, unquoted map[string]bool) {
	fmt.Fprintf(b, "# Step: %s\n", s.StepID)
	if s.Description != "" {
		for _, line := range strings.Split(strings.TrimSpace(s.Description), "\n") {
			fmt.Fprintf(b, "# %s\n", line)
		}
	}

	method, url, ok := resolveRequestLine(s.OperationID, s.Parameters, sources)
	if !ok {
		fmt.Fprintf(b, "# unresolved operationId: %s\n", s.OperationID)
		method = "GET"
		url = "{{baseUrl}}/__unresolved__/" + s.OperationID
	}
	fmt.Fprintf(b, "%s %s\n", method, url)

	writeHeaders(b, s.Parameters)
	writeQuery(b, s.Parameters)
	writeBody(b, s.RequestBody, unquoted)

	b.WriteString("\nHTTP *\n")
	writeAsserts(b, s.SuccessCriteria)
	writeCaptures(b, s.StepID, s.Outputs)
}

func writeHeaders(b *strings.Builder, params []model.Parameter) {
	for _, p := range params {
		if p.In != "header" {
			continue
		}
		fmt.Fprintf(b, "%s: %s\n", p.Name, renderValue(p.Value))
	}
}

func writeQuery(b *strings.Builder, params []model.Parameter) {
	first := true
	for _, p := range params {
		if p.In != "query" {
			continue
		}
		if first {
			b.WriteString("[QueryStringParams]\n")
			first = false
		}
		fmt.Fprintf(b, "%s: %s\n", p.Name, renderValue(p.Value))
	}
}

func writeBody(b *strings.Builder, body *model.RequestBody, unquoted map[string]bool) {
	if body == nil {
		return
	}
	fmt.Fprintf(b, "# requestBody content-type: %s\n", body.ContentType)
	fmt.Fprintf(b, "```\n%s\n```\n", serialiseBody(body, unquoted))
}

// serialiseBody turns the requestBody payload into the text of the Hurl
// body block, with runtime expressions translated to Hurl templates.
func serialiseBody(body *model.RequestBody, unquoted map[string]bool) string {
	if s, ok := body.Payload.(string); ok {
		return translateInlineExpr(s)
	}
	if strings.Contains(body.ContentType, "json") {
		if out, err := jsonBodyWithTemplates(body.Payload, unquoted); err == nil {
			return out
		}
	}
	return fmt.Sprintf("%v", translateBodyExprs(body.Payload))
}

// jsonBodyWithTemplates marshals the payload with expressions swapped
// for sentinels absent from the payload itself, so a literal string can
// never be mistaken for a template.
func jsonBodyWithTemplates(payload any, unquoted map[string]bool) (string, error) {
	probe, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	base := "__arazzo_tpl_"
	for strings.Contains(string(probe), base) {
		base = "_" + base
	}
	var repls []string
	swapped := swapBodyExprs(payload, unquoted, base, &repls)
	raw, err := json.MarshalIndent(swapped, "", "  ")
	if err != nil {
		return "", err
	}
	out := string(raw)
	for i, repl := range repls {
		out = strings.Replace(out, fmt.Sprintf("%q", fmt.Sprintf("%s%d__", base, i)), repl, 1)
	}
	return out, nil
}

func swapBodyExprs(v any, unquoted map[string]bool, base string, repls *[]string) any {
	switch t := v.(type) {
	case string:
		tpl := translateInlineExpr(t)
		if tpl == t {
			return t
		}
		repl := `"` + tpl + `"`
		if e := strings.TrimSpace(t); strings.HasPrefix(e, "$inputs.") && unquoted[strings.TrimPrefix(e, "$inputs.")] {
			repl = tpl
		}
		*repls = append(*repls, repl)
		return fmt.Sprintf("%s%d__", base, len(*repls)-1)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = swapBodyExprs(val, unquoted, base, repls)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = swapBodyExprs(val, unquoted, base, repls)
		}
		return out
	default:
		return v
	}
}

func translateBodyExprs(v any) any {
	switch t := v.(type) {
	case string:
		return translateInlineExpr(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = translateBodyExprs(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = translateBodyExprs(val)
		}
		return out
	default:
		return v
	}
}

func writeAsserts(b *strings.Builder, crits []model.SuccessCriterion) {
	if len(crits) == 0 {
		return
	}
	b.WriteString("[Asserts]\n")
	for _, c := range crits {
		fmt.Fprintf(b, "# %s\n", c.Condition)
	}
}

// writeCaptures emits the step's outputs as a Hurl [Captures] block.
// Capture variables are namespaced by step id (<stepId>_<outputName>)
// so later steps can resolve them with the same translation that
// $steps.<stepId>.outputs.<outputName> uses inline.
func writeCaptures(b *strings.Builder, stepID string, outs []model.OutputEntry) {
	if len(outs) == 0 {
		return
	}
	b.WriteString("[Captures]\n")
	for _, o := range outs {
		fmt.Fprintf(b, "%s_%s: %s\n", stepID, o.Name, translateCaptureExpr(o.Expression))
	}
}

// resolveRequestLine returns the HTTP method and full URL for the
// step, or ok=false when the operationId could not be resolved against
// the configured sources.
func resolveRequestLine(operationID string, params []model.Parameter, sources map[string]*oasresolver.Source) (method, url string, ok bool) {
	srcName, opID := parseOpRef(operationID, sources)
	if srcName == "" {
		return "", "", false
	}
	src, exists := sources[srcName]
	if !exists {
		return "", "", false
	}
	op, err := src.Resolve(opID)
	if err != nil {
		return "", "", false
	}
	// Always {{baseUrl}}, never op.BaseURL: requests stay environment-agnostic.
	path := substitutePathParams(op.Path, params)
	return op.Method, "{{baseUrl}}" + path, true
}

// parseOpRef recognises the two accepted forms of operationId:
//
//	"createOrder"                              -> short form
//	"$sourceDescriptions.shop-api.createOrder" -> qualified form
//
// Short form resolves only when exactly one source is configured; when
// multiple sources are present, the caller is expected to have used
// the qualified form (the linter enforces this upstream).
func parseOpRef(ref string, sources map[string]*oasresolver.Source) (srcName, opID string) {
	const prefix = "$sourceDescriptions."
	if strings.HasPrefix(ref, prefix) {
		rest := strings.TrimPrefix(ref, prefix)
		idx := strings.Index(rest, ".")
		if idx < 0 {
			return "", ""
		}
		return rest[:idx], rest[idx+1:]
	}
	if len(sources) == 1 {
		for name := range sources {
			return name, ref
		}
	}
	return "", ""
}

func substitutePathParams(path string, params []model.Parameter) string {
	for _, p := range params {
		if p.In != "path" {
			continue
		}
		placeholder := "{" + p.Name + "}"
		path = strings.ReplaceAll(path, placeholder, renderValue(p.Value))
	}
	return path
}

func renderValue(v any) string {
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return translateInlineExpr(s)
}

// translateInlineExpr maps an inline runtime expression to a Hurl
// template ($inputs.foo -> {{foo}}, $steps.s.outputs.o -> {{s_o}});
// anything else is returned unchanged so the user can spot it.
func translateInlineExpr(expr string) string {
	e := strings.TrimSpace(expr)
	switch {
	case strings.HasPrefix(e, "$inputs."):
		if name := strings.TrimPrefix(e, "$inputs."); isExprName(name) {
			return "{{" + name + "}}"
		}
		return expr
	case strings.HasPrefix(e, "$steps."):
		rest := strings.TrimPrefix(e, "$steps.")
		if step, out, ok := splitStepOutput(rest); ok && isExprName(step) && isExprName(out) {
			return "{{" + step + "_" + out + "}}"
		}
		return expr
	default:
		return expr
	}
}

func splitStepOutput(rest string) (step, out string, ok bool) {
	const sep = ".outputs."
	idx := strings.Index(rest, sep)
	if idx < 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+len(sep):], true
}

// isExprName reports whether s is a plain Arazzo name (letters, digits,
// '_' or '-'), the only form the generator can map to a Hurl variable.
func isExprName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// translateCaptureExpr maps an Arazzo output expression to a Hurl
// capture right-hand side. Recognised forms:
//
//	$response.body#/path  ->  jsonpath "$.path"
//	$statusCode           ->  status
//
// Anything else is returned as a Hurl comment so the user spots that
// the capture was not understood.
func translateCaptureExpr(expr string) string {
	e := strings.TrimSpace(expr)
	if strings.HasPrefix(e, "$response.body#/") {
		if path, ok := jsonPointerToJSONPath(strings.TrimPrefix(e, "$response.body#/")); ok {
			return `jsonpath "` + path + `"`
		}
		return "# unsupported: " + expr
	}
	if e == "$statusCode" {
		return "status"
	}
	return "# unsupported: " + expr
}

// jsonPointerToJSONPath converts the body of a JSON Pointer (after
// '#/') to a JSONPath expression rooted at $. Hurl's jsonpath grammar
// offers no escape for quotes or backslashes inside a bracket segment,
// so keys containing one are not representable and ok=false is
// returned.
func jsonPointerToJSONPath(ptr string) (string, bool) {
	var b strings.Builder
	b.WriteString("$")
	for _, seg := range strings.Split(ptr, "/") {
		seg = unescapeJSONPointer(seg)
		switch {
		case isUint(seg):
			fmt.Fprintf(&b, "[%s]", seg)
		case isJSONPathIdent(seg):
			b.WriteString(".")
			b.WriteString(seg)
		case strings.ContainsAny(seg, `'"\`):
			return "", false
		default:
			fmt.Fprintf(&b, "['%s']", seg)
		}
	}
	return b.String(), true
}

// unescapeJSONPointer decodes the RFC 6901 escape sequences inside a
// pointer segment: ~1 is '/', ~0 is '~'. ~1 must be decoded first so
// that ~01 yields the literal ~1.
func unescapeJSONPointer(seg string) string {
	return strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
}

// isJSONPathIdent reports whether s is safe to emit in JSONPath dot
// notation.
func isJSONPathIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func isUint(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
