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
	writeHeader(&b, wf)
	for i, step := range wf.Steps {
		if i > 0 {
			b.WriteString("\n")
		}
		writeStep(&b, step, sources)
	}
	return b.String(), nil
}

func writeHeader(b *strings.Builder, wf model.Workflow) {
	fmt.Fprintf(b, "# Workflow: %s\n", wf.WorkflowID)
	if wf.Summary != "" {
		fmt.Fprintf(b, "# %s\n", wf.Summary)
	}
	if len(wf.Inputs) > 0 {
		b.WriteString("#\n# Inputs (pass via `hurl --variable name=value`):\n")
		for _, in := range wf.Inputs {
			fmt.Fprintf(b, "#   - %s (%s)\n", in.Name, in.Type)
		}
	}
	b.WriteString("\n")
}

func writeStep(b *strings.Builder, s model.Step, sources map[string]*oasresolver.Source) {
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
	writeBody(b, s.RequestBody)

	b.WriteString("\nHTTP *\n")
	writeAsserts(b, s.SuccessCriteria)
	writeCaptures(b, s.Outputs)
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

func writeBody(b *strings.Builder, body *model.RequestBody) {
	if body == nil {
		return
	}
	fmt.Fprintf(b, "# requestBody content-type: %s\n", body.ContentType)
	fmt.Fprintf(b, "```\n%s\n```\n", serialiseBody(body))
}

// serialiseBody turns the workflow's requestBody payload into the text
// that goes inside the Hurl body block. JSON content types are marshalled
// through encoding/json so the result is valid JSON; raw string payloads
// are passed through; anything else falls back to Go's default formatting.
func serialiseBody(body *model.RequestBody) string {
	if s, ok := body.Payload.(string); ok {
		return s
	}
	if strings.Contains(body.ContentType, "json") {
		if raw, err := json.MarshalIndent(body.Payload, "", "  "); err == nil {
			return string(raw)
		}
	}
	return fmt.Sprintf("%v", body.Payload)
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

func writeCaptures(b *strings.Builder, outs []model.OutputEntry) {
	if len(outs) == 0 {
		return
	}
	b.WriteString("[Captures]\n")
	for _, o := range outs {
		fmt.Fprintf(b, "%s: %s\n", o.Name, translateCaptureExpr(o.Expression))
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
	path := substitutePathParams(op.Path, params)
	base := op.BaseURL
	if base == "" {
		base = "{{baseUrl}}"
	}
	return op.Method, base + path, true
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

// renderValue stringifies a parameter or body value, translating
// Arazzo runtime expressions into Hurl template placeholders where
// the value is a string.
func renderValue(v any) string {
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return translateInlineExpr(s)
}

// translateInlineExpr maps an Arazzo runtime expression used inline
// (in a parameter value, URL, header...) to a Hurl template. Anything
// not recognised is returned unchanged so the user can spot it.
func translateInlineExpr(expr string) string {
	e := strings.TrimSpace(expr)
	switch {
	case strings.HasPrefix(e, "$inputs."):
		return "{{" + strings.TrimPrefix(e, "$inputs.") + "}}"
	case strings.HasPrefix(e, "$steps."):
		rest := strings.TrimPrefix(e, "$steps.")
		return "{{" + strings.ReplaceAll(rest, ".outputs.", "_") + "}}"
	default:
		return expr
	}
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
		ptr := strings.TrimPrefix(e, "$response.body#/")
		return `jsonpath "$.` + strings.ReplaceAll(ptr, "/", ".") + `"`
	}
	if e == "$statusCode" {
		return "status"
	}
	return "# unsupported: " + expr
}
