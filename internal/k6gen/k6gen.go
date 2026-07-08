// Package k6gen renders an Arazzo workflow as a k6 performance test
// script (.k6.js).
//
// Mapping:
//
//	step                      -> one http.request(...) call
//	step.outputs              -> const captures from the response
//	step.successCriteria      -> check() entries (status criteria only;
//	                             other conditions are emitted as comments)
//	parameters in=header      -> the request headers object
//	parameters in=query       -> the request URL query string
//	parameters in=path        -> substituted into the request URL
//	parameters in=cookie      -> the request cookies object
//	parameters in=querystring -> appended to the request URL
//
// Operation resolution goes through the oasresolver package, exactly as
// the e2e generator does: callers pass a map of source-description name
// to *oasresolver.Source. Short operationId forms ("listUsers") resolve
// when exactly one source is configured; the qualified form
// ("$sourceDescriptions.<name>.<id>") works with any number of sources,
// and operationPath references resolve their JSON pointer against the
// named source. Unresolvable steps emit a placeholder URL and a comment
// naming the reference so the script stays valid JavaScript a human can
// patch. Steps that invoke another workflow (workflowId) emit an
// explicit not-supported comment and no request: a nested workflow is
// not one HTTP call, and a placeholder request would fail against the
// real endpoint.
//
// Arazzo runtime expressions are translated to JavaScript identifiers:
// $inputs.foo becomes the foo constant (read from __ENV), and
// $steps.s.outputs.o becomes the s_o capture from an earlier step; the
// spec's embedded {$expr} form becomes a template literal. Names that
// are not valid JS identifiers (hyphens, leading digits) are sanitised
// consistently on both the declaration and the reference.
//
// The host is never hard-coded: every request is prefixed with the
// BASE_URL constant, read from `__ENV.BASE_URL` so the same script runs
// against any environment via `k6 run -e BASE_URL=<endpoint>`. The
// OpenAPI `servers:` URL, when present, is the documented default.
//
// The load profile (virtual users, duration) and thresholds are not part
// of Arazzo; they are supplied by the caller through Options and written
// into the k6 `options` export.
package k6gen

import (
	"bytes"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/emmanuelperu/arazzo-maestro/internal/expr"
	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
	"github.com/emmanuelperu/arazzo-maestro/internal/payload"
)

// Options carries the load profile and thresholds that Arazzo does not
// describe. Thresholds maps a k6 metric name to its list of threshold
// expressions, e.g. {"http_req_duration": {"p(95)<500"}}.
type Options struct {
	VUs        int
	Duration   string
	Thresholds map[string][]string
}

// Generate renders the workflow as a k6 script.
//
// sources is keyed by Arazzo sourceDescription name; the caller is
// expected to have loaded each one via oasresolver.Load. A nil or empty
// map is accepted, in which case every step is rendered with an
// unresolved placeholder.
func Generate(wf model.Workflow, sources map[string]*oasresolver.Source, opts Options) (string, error) {
	var b strings.Builder
	writeHeader(&b, wf, defaultBaseURL(wf, sources))
	writeImports(&b)
	writeBaseURL(&b, defaultBaseURL(wf, sources))
	writeInputs(&b, wf.Inputs, subAccessedInputs(wf))
	writeOptions(&b, opts)
	writeDefaultFunc(&b, wf, sources)
	return b.String(), nil
}

func writeHeader(b *strings.Builder, wf model.Workflow, defaultBase string) {
	fmt.Fprintf(b, "// Workflow: %s\n", wf.WorkflowID)
	if wf.Summary != "" {
		fmt.Fprintf(b, "// %s\n", wf.Summary)
	}
	b.WriteString("//\n// k6 performance test generated from an Arazzo workflow.\n")
	b.WriteString("// Base URL: override with `k6 run -e BASE_URL=<endpoint> <file>`.\n")
	if defaultBase != "" {
		fmt.Fprintf(b, "//   default (OpenAPI servers): %s\n", defaultBase)
	}
	if len(wf.Inputs) > 0 {
		b.WriteString("//\n// Inputs: override each with `-e <name>=<value>`.\n")
		for _, in := range wf.Inputs {
			fmt.Fprintf(b, "//   - %s (%s)\n", in.Name, in.TypeLabel())
		}
	}
	b.WriteString("\n")
}

func writeImports(b *strings.Builder) {
	b.WriteString("import http from 'k6/http';\n")
	b.WriteString("import { check } from 'k6';\n\n")
}

func writeBaseURL(b *strings.Builder, defaultBase string) {
	fmt.Fprintf(b, "const BASE_URL = __ENV.BASE_URL || %s;\n\n", jsString(defaultBase))
}

// writeInputs declares one constant per workflow input, read from the
// matching environment variable with the Arazzo default (or empty
// string) as fallback. The constant name is the sanitised input name so
// it matches what translateInlineExpr emits for $inputs.<name>. When
// some reference sub-accesses an input with a #/<json-pointer> suffix,
// the asJson helper is emitted: parsing happens per reference, so a
// whole-value use of the same input keeps its plain string.
func writeInputs(b *strings.Builder, inputs []model.InputProperty, subAccessed bool) {
	if subAccessed {
		b.WriteString("// asJson parses string values so #/<json-pointer> sub-accesses can navigate them.\n")
		b.WriteString("function asJson(v) { return typeof v === \"string\" ? JSON.parse(v) : v; }\n\n")
	}
	if len(inputs) == 0 {
		return
	}
	declared := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		ident := jsIdent(in.Name)
		if declared[ident] {
			// Sanitisation can collide (e.g. "user.name" and "user_name"
			// both become user_name); redeclaring would be a SyntaxError.
			fmt.Fprintf(b, "// input %s collides with an earlier declaration after sanitisation\n", jsString(in.Name))
			continue
		}
		declared[ident] = true
		fmt.Fprintf(b, "const %s = __ENV[%s] || %s;\n", ident, jsString(in.Name), jsDefault(in.Default))
	}
	b.WriteString("\n")
}

// subAccessedInputs reports whether any reference sub-accesses an input
// with a #/<json-pointer> suffix, scanning the same post-replacement
// values the steps serialise.
func subAccessedInputs(wf model.Workflow) bool {
	for _, step := range wf.Steps {
		if step.WorkflowID != "" {
			continue
		}
		step.Parameters = resolvedParameters(step.Parameters)
		var effective any
		if step.RequestBody != nil {
			effective, _ = payload.Apply(step.RequestBody.Payload, step.RequestBody.Replacements)
		}
		for _, value := range inlineValues(step, effective) {
			for _, ref := range expr.CollectRefs(value) {
				if e := expr.Parse(ref); e.Kind == expr.KindInput && e.HasPointer {
					return true
				}
			}
		}
	}
	return false
}

func writeOptions(b *strings.Builder, opts Options) {
	b.WriteString("export const options = {\n")
	fmt.Fprintf(b, "  vus: %d,\n", opts.VUs)
	fmt.Fprintf(b, "  duration: %s,\n", jsString(opts.Duration))
	if len(opts.Thresholds) > 0 {
		b.WriteString("  thresholds: {\n")
		// Sort metrics so the output is deterministic regardless of map
		// iteration order.
		metrics := make([]string, 0, len(opts.Thresholds))
		for m := range opts.Thresholds {
			metrics = append(metrics, m)
		}
		sort.Strings(metrics)
		for _, m := range metrics {
			exprs := make([]string, 0, len(opts.Thresholds[m]))
			for _, e := range opts.Thresholds[m] {
				exprs = append(exprs, jsString(e))
			}
			fmt.Fprintf(b, "    %s: [%s],\n", jsString(m), strings.Join(exprs, ", "))
		}
		b.WriteString("  },\n")
	}
	b.WriteString("};\n\n")
}

func writeDefaultFunc(b *strings.Builder, wf model.Workflow, sources map[string]*oasresolver.Source) {
	b.WriteString("export default function () {\n")
	// Inputs first, then each step's captures: body expressions only
	// translate to identifiers in here, never to undeclared constants.
	declared := make(map[string]bool, len(wf.Inputs))
	for _, in := range wf.Inputs {
		declared[jsIdent(in.Name)] = true
	}
	for i, step := range wf.Steps {
		if i > 0 {
			b.WriteString("\n")
		}
		writeStep(b, step, sources, declared)
	}
	b.WriteString("}\n")
}

func writeStep(b *strings.Builder, s model.Step, sources map[string]*oasresolver.Source, declared map[string]bool) {
	fmt.Fprintf(b, "  // Step: %s\n", s.StepID)
	if s.Description != "" {
		for _, line := range strings.Split(strings.TrimSpace(s.Description), "\n") {
			fmt.Fprintf(b, "  // %s\n", line)
		}
	}
	if s.WorkflowID != "" {
		fmt.Fprintf(b, "  // not supported: this step invokes workflow %q (workflowId); no request generated\n", s.WorkflowID)
		if len(s.Parameters) > 0 {
			b.WriteString("  // warning: parameters are not forwarded to the invoked workflow\n")
		}
		if len(s.Outputs) > 0 {
			fmt.Fprintf(b, "  // warning: outputs %s are not captured; later references to them stay unresolved\n", outputNames(s.Outputs))
		}
		return
	}

	// Apply payload replacements once, up front: the unsupported-expression
	// scan and the serialised body must agree on the same payload.
	var effective any
	var unresolved []string
	if s.RequestBody != nil {
		effective, unresolved = payload.Apply(s.RequestBody.Payload, s.RequestBody.Replacements)
	}
	for _, e := range expr.UnsupportedInline(inlineValues(s, effective), translateInlineExpr) {
		fmt.Fprintf(b, "  // unsupported expression (not translated): %s\n", e)
	}
	for _, p := range s.Parameters {
		if p.Reference != "" {
			fmt.Fprintf(b, "  // unresolved component reference (parameter dropped): %s\n", p.Reference)
		}
	}
	// s is a copy: dropping unresolved entries here keeps every writer
	// below (headers, query, path, cookies, body scan) consistent with
	// the dropped-comment above.
	s.Parameters = resolvedParameters(s.Parameters)

	op, method, url, ok := resolveRequestLine(s, sources, declared)
	if !ok {
		method = "GET"
		if s.OperationPath != "" {
			fmt.Fprintf(b, "  // unresolved operationPath: %s\n", s.OperationPath)
			// The raw reference carries JSON-pointer escapes and braces, so
			// the placeholder URL names the step instead; escaping keeps the
			// stepId from breaking the backtick URL literal.
			url = "${BASE_URL}/__unresolved__/" + neturl.PathEscape(s.StepID)
		} else {
			fmt.Fprintf(b, "  // unresolved operationId: %s\n", s.OperationID)
			url = "${BASE_URL}/__unresolved__/" + s.OperationID
		}
	}
	url += queryString(s.Parameters, declared)
	if qs := querystringValue(s.Parameters, declared); qs != "" {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url += sep + qs
	}

	// Effective content type: explicit Arazzo value, else the targeted
	// operation's declared type. k6 sends a string body as text/plain by
	// default, so a JSON body needs the header set explicitly.
	ct, ctKnown := "", false
	if s.RequestBody != nil {
		ct, ctKnown = oasresolver.EffectiveContentType(s.RequestBody.ContentType, op.RequestContentTypes())
	}

	resVar := jsIdent(s.StepID) + "Res"
	bodyArg := writeBody(b, s.StepID, s.RequestBody, effective, unresolved, ct, ctKnown, declared)
	reqParams := "{ headers: " + headersObject(s.Parameters, ct, ctKnown, declared)
	if c := cookiesObject(s.Parameters, declared); c != "" {
		reqParams += ", cookies: " + c
	}
	reqParams += " }"
	fmt.Fprintf(b, "  const %s = http.request('%s', `%s`, %s, %s);\n",
		resVar, method, url, bodyArg, reqParams)

	writeChecks(b, resVar, s.StepID, s.SuccessCriteria)
	writeCaptures(b, resVar, s.StepID, s.Outputs, declared)
}

// writeBody declares the request body constant when the step has one and
// returns the argument to pass as the http.request body (the constant
// name for a raw string, JSON.stringify(...) for a structured payload,
// or "null" when there is no body). Runtime expressions inside the
// payload are translated to the JS identifiers holding their values.
func writeBody(b *strings.Builder, stepID string, body *model.RequestBody, effective any, unresolved []string, ct string, ctKnown bool, declared map[string]bool) string {
	if body == nil {
		return "null"
	}
	if ctKnown {
		fmt.Fprintf(b, "  // requestBody content-type: %s\n", ct)
	} else {
		b.WriteString("  // requestBody content-type: unknown (omitted by Arazzo; the operation declares none or several non-JSON types)\n")
	}
	for _, r := range body.Replacements {
		val, _ := jsonMarshal(r.Value, "", "")
		fmt.Fprintf(b, "  // replacement: %s = %s\n", r.Target, val)
	}
	for _, u := range unresolved {
		fmt.Fprintf(b, "  // warning: replacement target %q did not resolve in the payload\n", u)
	}
	name := jsIdent(stepID) + "Body"
	if s, ok := effective.(string); ok {
		value, _ := jsBodyValue(s, declared) // a string value never errors
		fmt.Fprintf(b, "  const %s = %s;\n", name, value)
		return name
	}
	if raw, err := jsBodyValue(effective, declared); err == nil {
		fmt.Fprintf(b, "  const %s = %s;\n", name, raw)
		return "JSON.stringify(" + name + ")"
	}
	fmt.Fprintf(b, "  const %s = %s;\n", name, jsString(fmt.Sprintf("%v", effective)))
	return name
}

// jsBodyValue renders a requestBody value as a JS expression: declared
// runtime expressions are swapped for sentinels before a single JSON
// marshal (indented JSON is valid JS), then the quoted sentinels become
// bare identifiers.
func jsBodyValue(v any, declared map[string]bool) (string, error) {
	// A sentinel base absent from the payload guarantees a literal can
	// never be mistaken for a swapped expression.
	probe, err := jsonMarshal(v, "", "")
	if err != nil {
		return "", err
	}
	base := "__arazzo_expr_"
	for strings.Contains(probe, base) {
		base = "_" + base
	}
	var idents []string
	swapped := swapBodyExprs(v, declared, base, &idents)
	raw, err := jsonMarshal(swapped, "  ", "  ")
	if err != nil {
		return "", err
	}
	for i, ident := range idents {
		raw = strings.Replace(raw, `"`+bodyExprSentinel(base, i)+`"`, ident, 1)
	}
	return raw, nil
}

func swapBodyExprs(v any, declared map[string]bool, base string, repls *[]string) any {
	switch t := v.(type) {
	case string:
		if jsExpr, isExpr := exprIdent(t); isExpr && declared[baseIdent(jsExpr)] {
			*repls = append(*repls, jsExpr)
			return bodyExprSentinel(base, len(*repls)-1)
		}
		if lit, ok := jsTemplateLiteral(t, declared); ok {
			*repls = append(*repls, lit)
			return bodyExprSentinel(base, len(*repls)-1)
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = swapBodyExprs(val, declared, base, repls)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = swapBodyExprs(val, declared, base, repls)
		}
		return out
	default:
		return v
	}
}

// embeddedExprRe matches the spec's embedded form: a runtime expression
// wrapped in {} curly braces inside a string value.
var embeddedExprRe = regexp.MustCompile(`\{(\$[^{}]+)\}`)

// jsTemplateLiteral renders a string carrying embedded {$expr}
// occurrences as a JS template literal (`Total: ${total}`). Only
// expressions resolving to a declared identifier are interpolated;
// ok is false when none does.
func jsTemplateLiteral(s string, declared map[string]bool) (string, bool) {
	locs := embeddedExprRe.FindAllStringSubmatchIndex(s, -1)
	if len(locs) == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("`")
	last := 0
	interpolated := false
	for _, loc := range locs {
		expr := s[loc[2]:loc[3]]
		jsExpr, isExpr := exprIdent(expr)
		if !isExpr || !declared[baseIdent(jsExpr)] {
			continue
		}
		b.WriteString(jsTemplateEscaper.Replace(s[last:loc[0]]))
		b.WriteString("${" + jsExpr + "}")
		last = loc[1]
		interpolated = true
	}
	if !interpolated {
		return "", false
	}
	b.WriteString(jsTemplateEscaper.Replace(s[last:]))
	b.WriteString("`")
	return b.String(), true
}

// jsTemplateEscaper protects the characters that JS template literals
// treat as syntax.
var jsTemplateEscaper = strings.NewReplacer("\\", "\\\\", "`", "\\`", "${", "\\${")

// bodyExprSentinel is plain ASCII so its JSON encoding is the quoted
// sentinel itself.
func bodyExprSentinel(base string, i int) string {
	return fmt.Sprintf("%s%d__", base, i)
}

// writeChecks emits a single check() call grouping the step's success
// criteria. Status-code conditions become real predicates; conditions in
// the broader Arazzo mini-language are emitted as comments rather than
// guessed at, so the script never asserts something it did not actually
// translate.
func writeChecks(b *strings.Builder, resVar, stepID string, crits []model.SuccessCriterion) {
	if len(crits) == 0 {
		return
	}
	var predicates []string
	var unsupported []string
	for _, c := range crits {
		// Only the simple mini-language is translated; a typed criterion
		// (regex/jsonpath/xpath) is in another language entirely.
		if pred, ok := translateCondition(c.Condition); ok && c.IsSimple() {
			predicates = append(predicates, fmt.Sprintf("    %s: (r) => %s,",
				jsString(stepID+": "+c.Condition), pred))
		} else {
			unsupported = append(unsupported, c.Describe())
		}
	}
	for _, u := range unsupported {
		fmt.Fprintf(b, "  // successCriteria (not translated): %s\n", u)
	}
	if len(predicates) == 0 {
		return
	}
	fmt.Fprintf(b, "  check(%s, {\n", resVar)
	for _, p := range predicates {
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("  });\n")
}

// writeCaptures declares one constant per output, namespaced by step id
// (<stepId>_<outputName>) so a later step's $steps.<stepId>.outputs.<o>
// reference resolves to the same identifier.
func writeCaptures(b *strings.Builder, resVar, stepID string, outs []model.OutputEntry, declared map[string]bool) {
	for _, o := range outs {
		name := jsIdent(stepID) + "_" + jsIdent(o.Name)
		fmt.Fprintf(b, "  const %s = %s;\n", name, translateCaptureExpr(resVar, o.Expression))
		declared[name] = true
	}
}

// resolveRequestLine returns the HTTP method and URL template (a JS
// backtick-literal body, BASE_URL-prefixed) for the step, or ok=false
// when its operation reference could not be resolved.
func resolveRequestLine(s model.Step, sources map[string]*oasresolver.Source, declared map[string]bool) (op oasresolver.Operation, method, url string, ok bool) {
	op, ok = oasresolver.ResolveStepOperation(s, sources)
	if !ok {
		return oasresolver.Operation{}, "", "", false
	}
	// Always BASE_URL, never op.BaseURL: requests stay environment-agnostic.
	return op, op.Method, "${BASE_URL}" + substitutePathParams(op.Path, s.Parameters, declared), true
}

// defaultBaseURL returns the OpenAPI servers URL backing the workflow's
// first resolvable step, or "" when none resolves. Documentation only:
// requests always use the BASE_URL constant.
func defaultBaseURL(wf model.Workflow, sources map[string]*oasresolver.Source) string {
	for _, s := range wf.Steps {
		op, ok := oasresolver.ResolveStepOperation(s, sources)
		if ok && op.BaseURL != "" {
			return op.BaseURL
		}
	}
	return ""
}

// resolvedParameters filters out unresolved Reusable Object entries:
// they are announced as dropped and must not reach the request.
func resolvedParameters(params []model.Parameter) []model.Parameter {
	out := make([]model.Parameter, 0, len(params))
	for _, p := range params {
		if p.Reference == "" {
			out = append(out, p)
		}
	}
	return out
}

// outputNames joins the declared output names for a comment.
func outputNames(outs []model.OutputEntry) string {
	names := make([]string, len(outs))
	for i, o := range outs {
		names[i] = o.Name
	}
	return strings.Join(names, ", ")
}

func substitutePathParams(path string, params []model.Parameter, declared map[string]bool) string {
	for _, p := range params {
		if p.In != "path" {
			continue
		}
		path = strings.ReplaceAll(path, "{"+p.Name+"}", urlValue(p.Value, declared))
	}
	return path
}

// queryString builds the URL query suffix (?a=1&b=2) from the step's
// query parameters, or "" when there are none.
func queryString(params []model.Parameter, declared map[string]bool) string {
	var parts []string
	for _, p := range params {
		if p.In != "query" {
			continue
		}
		parts = append(parts, p.Name+"="+urlValue(p.Value, declared))
	}
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}

// headersObject renders the step's header parameters as a JS object
// literal for the request params. When the effective content type is
// known and the step declares no explicit Content-Type header, it is
// added so k6 does not fall back to text/plain for a string body. An
// empty set yields "{}".
func headersObject(params []model.Parameter, contentType string, ctKnown bool, declared map[string]bool) string {
	var entries []string
	hasCT := false
	for _, p := range params {
		if p.In != "header" {
			continue
		}
		if strings.EqualFold(p.Name, "content-type") {
			hasCT = true
		}
		entries = append(entries, fmt.Sprintf("%s: %s", jsString(p.Name), headerValue(p.Value, declared)))
	}
	if ctKnown && !hasCT {
		entries = append([]string{fmt.Sprintf("%s: %s", jsString("Content-Type"), jsString(contentType))}, entries...)
	}
	if len(entries) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(entries, ", ") + " }"
}

// cookiesObject renders the step's cookie parameters as a JS object
// literal for the request params, or "" when there are none.
func cookiesObject(params []model.Parameter, declared map[string]bool) string {
	var entries []string
	for _, p := range params {
		if p.In != "cookie" {
			continue
		}
		entries = append(entries, fmt.Sprintf("%s: %s", jsString(p.Name), headerValue(p.Value, declared)))
	}
	if len(entries) == 0 {
		return ""
	}
	return "{ " + strings.Join(entries, ", ") + " }"
}

// querystringValue returns the rendered whole-query-string parameter
// for interpolation inside the URL literal, or "".
func querystringValue(params []model.Parameter, declared map[string]bool) string {
	for _, p := range params {
		if p.In == "querystring" {
			return urlValue(p.Value, declared)
		}
	}
	return ""
}

// urlValue renders a path or query parameter for interpolation inside a
// backtick URL literal: declared runtime expressions become
// ${identifier} (whole-string or embedded {$expr}), anything else is
// inlined verbatim.
func urlValue(v any, declared map[string]bool) string {
	if s, ok := v.(string); ok {
		if jsExpr, isExpr := exprIdent(s); isExpr && declared[baseIdent(jsExpr)] {
			return "${" + jsExpr + "}"
		}
		// The value lands inside the request's backtick URL literal, so
		// literal text must be escaped; the {$expr} pattern survives the
		// escaper untouched.
		return embeddedExprRe.ReplaceAllStringFunc(jsTemplateEscaper.Replace(s), func(m string) string {
			if jsExpr, isExpr := exprIdent(m[1 : len(m)-1]); isExpr && declared[baseIdent(jsExpr)] {
				return "${" + jsExpr + "}"
			}
			return m
		})
	}
	return fmt.Sprintf("%v", v)
}

// headerValue renders a header parameter as a JS expression: declared
// runtime expressions become the bare identifier (whole-string) or a
// template literal (embedded {$expr}), plain strings are quoted.
func headerValue(v any, declared map[string]bool) string {
	if s, ok := v.(string); ok {
		if jsExpr, isExpr := exprIdent(s); isExpr && declared[baseIdent(jsExpr)] {
			return jsExpr
		}
		if lit, ok := jsTemplateLiteral(s, declared); ok {
			return lit
		}
		return jsString(s)
	}
	return fmt.Sprintf("%v", v)
}

// translateCaptureExpr maps an Arazzo output expression to the JS that
// reads it from the response. Recognised forms:
//
//	$response.body        ->  res.json()         (whole parsed body)
//	$response.body#/path  ->  res.json('path')   (gjson selector)
//	$statusCode           ->  res.status
//
// Anything else yields null with a trailing comment so the unsupported
// expression is visible in the script.
func translateCaptureExpr(resVar, s string) string {
	switch e := expr.Parse(s); e.Kind {
	case expr.KindResponseBody:
		if e.HasPointer {
			return fmt.Sprintf("%s.json(%s)", resVar, jsString(jsonPointerToGJSON(e.Pointer)))
		}
		return resVar + ".json()"
	case expr.KindStatusCode:
		return resVar + ".status"
	default:
		return "null; // unsupported: " + s
	}
}

// translateCondition maps the status-code subset of the Arazzo success
// criteria mini-language to a JS boolean expression over the response r.
// It returns ok=false for any condition it does not understand.
func translateCondition(cond string) (string, bool) {
	c := strings.TrimSpace(cond)
	if !strings.HasPrefix(c, "$statusCode") {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(c, "$statusCode"))
	for _, op := range []struct{ arazzo, js string }{
		{"==", "==="}, {"!=", "!=="}, {">=", ">="}, {"<=", "<="}, {">", ">"}, {"<", "<"},
	} {
		if strings.HasPrefix(rest, op.arazzo) {
			operand := strings.TrimSpace(strings.TrimPrefix(rest, op.arazzo))
			if !isNumber(operand) {
				return "", false
			}
			return fmt.Sprintf("r.status %s %s", op.js, operand), true
		}
	}
	return "", false
}

// exprIdent reports whether s is an inline Arazzo runtime expression and,
// if so, the JS identifier it maps to.
func exprIdent(s string) (string, bool) {
	out := translateInlineExpr(s)
	return out, out != s
}

// inlineValues lists the values a step emits inline (parameter values and
// the request body after replacements) for the unsupported-expression
// scan, so the scan sees exactly what gets serialised. A recognised form
// that merely references an undeclared step is not flagged: that is a
// linter concern, not an untranslatable form.
func inlineValues(s model.Step, effectiveBody any) []any {
	values := make([]any, 0, len(s.Parameters)+1)
	for _, p := range s.Parameters {
		values = append(values, p.Value)
	}
	if s.RequestBody != nil {
		values = append(values, effectiveBody)
	}
	return values
}

// translateInlineExpr maps an inline runtime expression to the JS
// expression reading its value ($inputs.foo -> foo, $steps.s.outputs.o
// -> s_o); anything else is returned unchanged. A #/<json-pointer>
// suffix becomes a bracket chain on the parsed value (arrays accept
// string indices in JS, so every segment is emitted as a string key).
func translateInlineExpr(s string) string {
	e := expr.Parse(s)
	var base string
	switch e.Kind {
	case expr.KindInput:
		base = jsIdent(e.Name)
	case expr.KindStepOutput:
		base = jsIdent(e.Name) + "_" + jsIdent(e.OutputName)
	default:
		return s
	}
	if !e.HasPointer {
		return base
	}
	var b strings.Builder
	if e.Kind == expr.KindInput {
		// Inputs come from __ENV as strings: parse per reference so a
		// whole-value use of the same input stays a plain string.
		b.WriteString("asJson(" + base + ")")
	} else {
		b.WriteString(base)
	}
	for _, seg := range strings.Split(e.Pointer, "/") {
		b.WriteString("[" + jsString(expr.UnescapeJSONPointer(seg)) + "]")
	}
	return b.String()
}

// baseIdent returns the identifier a translated expression reads, which
// is what must be declared: the bracket chain of a pointer sub-access
// starts at the same base identifier, possibly wrapped in asJson().
func baseIdent(jsExpr string) string {
	if i := strings.IndexByte(jsExpr, '['); i >= 0 {
		jsExpr = jsExpr[:i]
	}
	jsExpr = strings.TrimPrefix(jsExpr, "asJson(")
	return strings.TrimSuffix(jsExpr, ")")
}

// jsonPointerToGJSON converts the body of a JSON Pointer (after '#/')
// to the gjson selector k6's Response.json() expects.
func jsonPointerToGJSON(ptr string) string {
	segs := strings.Split(ptr, "/")
	for i, seg := range segs {
		segs[i] = gjsonEscaper.Replace(expr.UnescapeJSONPointer(seg))
	}
	return strings.Join(segs, ".")
}

// gjsonEscaper protects every character that gjson treats as syntax
// inside a path segment, including its own escape character.
var gjsonEscaper = strings.NewReplacer(
	`\`, `\\`, ".", `\.`, "*", `\*`, "?", `\?`, "|", `\|`, "#", `\#`,
)

// jsIdent turns an arbitrary name into a valid JS identifier: every
// character outside [A-Za-z0-9_] becomes '_', and a leading digit is
// prefixed with '_'. Applied consistently to declarations and references.
func jsIdent(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				b.WriteRune('_')
			}
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

// jsString encodes v as a JS string literal (JSON string encoding is
// valid JS).
func jsString(v string) string {
	raw, err := jsonMarshal(v, "", "")
	if err != nil {
		return `""`
	}
	return raw
}

// jsDefault renders an input default as a JS literal, falling back to an
// empty string when the workflow declares no default.
func jsDefault(v any) string {
	if v == nil {
		return `''`
	}
	if raw, err := jsonMarshal(v, "", ""); err == nil {
		return raw
	}
	return `''`
}

// jsonMarshal encodes v as JSON with HTML escaping disabled, so JS
// operators like '<' in k6 threshold expressions survive verbatim; a
// non-empty indent pretty-prints (indented JSON is valid JS).
func jsonMarshal(v any, prefix, indent string) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent(prefix, indent)
	}
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

func isNumber(s string) bool {
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
