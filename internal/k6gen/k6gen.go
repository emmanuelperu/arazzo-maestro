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
// spec's embedded {$expr} form becomes a template literal. Identifiers
// are assigned at declaration time (inputs first, then each step's
// captures) in one table; sanitisation collisions get a numeric suffix,
// and references translate by exact Arazzo name, so a name that is never
// declared stays visibly untranslated instead of silently reading
// another name that sanitises to the same identifier.
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
	base := oasresolver.DefaultBaseURL(wf, sources)
	ids := declareInputs(wf.Inputs)
	var b strings.Builder
	writeHeader(&b, wf, base)
	writeImports(&b)
	writeBaseURL(&b, base)
	writeInputs(&b, wf.Inputs, ids, subAccessedInputs(wf, ids))
	writeOptions(&b, opts)
	writeDefaultFunc(&b, wf, sources, ids)
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

// identTable assigns each declarable Arazzo name (workflow input, step
// output capture) its JS identifier. Declarations claim identifiers in
// document order; a sanitisation collision (e.g. "user.name" after
// "user_name") gets a numeric suffix so every input keeps its own
// constant and environment variable. References translate through the
// table by exact Arazzo name: a name that is never declared stays
// visibly untranslated instead of silently reading another name that
// sanitises to the same identifier.
type identTable struct {
	exprs map[string]string // canonical expression -> declared identifier
	taken map[string]bool
}

func newIdentTable() *identTable {
	return &identTable{
		exprs: make(map[string]string),
		// Identifiers every generated script may declare itself.
		taken: map[string]bool{"asJson": true, "http": true, "check": true, "BASE_URL": true},
	}
}

// declare assigns key its sanitised identifier, suffixing a counter when
// an earlier declaration already took it, and returns the identifier.
func (t *identTable) declare(key, base string) string {
	ident := base
	for n := 2; t.taken[ident]; n++ {
		ident = fmt.Sprintf("%s_%d", base, n)
	}
	t.taken[ident] = true
	t.exprs[key] = ident
	return ident
}

func (t *identTable) lookup(key string) (string, bool) {
	ident, ok := t.exprs[key]
	return ident, ok
}

func inputKey(name string) string { return "$inputs." + name }

func stepOutputKey(stepID, output string) string {
	return "$steps." + stepID + ".outputs." + output
}

// declareInputs registers every workflow input, in declaration order, so
// the input constants and their references agree on the identifiers.
func declareInputs(inputs []model.InputProperty) *identTable {
	t := newIdentTable()
	for _, in := range inputs {
		t.declare(inputKey(in.Name), jsIdent(in.Name))
	}
	return t
}

// writeInputs declares one constant per workflow input, read from the
// matching environment variable with the Arazzo default (or empty
// string) as fallback. When some reference sub-accesses an input with a
// #/<json-pointer> suffix, the asJson helper is emitted: parsing happens
// per reference, so a whole-value use of the same input keeps its plain
// string.
func writeInputs(b *strings.Builder, inputs []model.InputProperty, ids *identTable, subAccessed bool) {
	if subAccessed {
		b.WriteString("// asJson parses string values so #/<json-pointer> sub-accesses can navigate them.\n")
		b.WriteString("function asJson(v) { return typeof v === \"string\" ? JSON.parse(v) : v; }\n\n")
	}
	if len(inputs) == 0 {
		return
	}
	for _, in := range inputs {
		ident, _ := ids.lookup(inputKey(in.Name))
		if ident != jsIdent(in.Name) {
			fmt.Fprintf(b, "// input %s: identifier %s was already taken, declared as %s\n", jsString(in.Name), jsIdent(in.Name), ident)
		}
		fmt.Fprintf(b, "const %s = __ENV[%s] || %s;\n", ident, jsString(in.Name), jsDefault(in.Default))
	}
	b.WriteString("\n")
}

// subAccessedInputs reports whether any reference sub-accesses a declared
// input with a #/<json-pointer> suffix, scanning the same
// post-replacement values the steps serialise.
func subAccessedInputs(wf model.Workflow, ids *identTable) bool {
	for _, step := range wf.Steps {
		if step.WorkflowID != "" {
			continue
		}
		step.Parameters = model.ResolvedParameters(step.Parameters)
		var effective any
		if step.RequestBody != nil {
			effective, _ = payload.Apply(step.RequestBody.Payload, step.RequestBody.Replacements)
		}
		for _, value := range model.InlineValues(step, effective) {
			for _, ref := range expr.CollectRefs(value) {
				e := expr.Parse(ref)
				if e.Kind != expr.KindInput || !e.HasPointer {
					continue
				}
				if _, declared := ids.lookup(inputKey(e.Name)); declared {
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

func writeDefaultFunc(b *strings.Builder, wf model.Workflow, sources map[string]*oasresolver.Source, ids *identTable) {
	b.WriteString("export default function () {\n")
	for i, step := range wf.Steps {
		if i > 0 {
			b.WriteString("\n")
		}
		writeStep(b, step, sources, ids)
	}
	b.WriteString("}\n")
}

func writeStep(b *strings.Builder, s model.Step, sources map[string]*oasresolver.Source, ids *identTable) {
	fmt.Fprintf(b, "  // Step: %s\n", s.StepID)
	if s.Description != "" {
		for _, line := range strings.Split(strings.TrimSpace(s.Description), "\n") {
			fmt.Fprintf(b, "  // %s\n", line)
		}
	}
	if s.WorkflowID != "" {
		fmt.Fprintf(b, "  // not supported: this step invokes workflow %q (workflowId); no request generated\n", s.WorkflowID)
		// Workflow-level parameter defaults reach every step, so only the
		// step's own parameters justify the warning.
		if len(model.OwnParameters(s.Parameters)) > 0 {
			b.WriteString("  // warning: parameters are not forwarded to the invoked workflow\n")
		}
		if len(s.Outputs) > 0 {
			fmt.Fprintf(b, "  // warning: outputs %s are not captured; later references to them stay unresolved\n", model.OutputNames(s.Outputs))
		}
		return
	}

	for _, p := range s.Parameters {
		if p.Reference != "" {
			fmt.Fprintf(b, "  // unresolved component reference (parameter dropped): %s\n", p.Reference)
		}
	}
	// s is a copy: dropping unresolved entries here, before the scan,
	// keeps the unsupported-expression comments and every writer below
	// (headers, query, path, cookies, body) consistent with the
	// dropped-comment above.
	s.Parameters = model.ResolvedParameters(s.Parameters)

	// Apply payload replacements once, up front: the unsupported-expression
	// scan and the serialised body must agree on the same payload.
	var effective any
	var unresolved []string
	if s.RequestBody != nil {
		effective, unresolved = payload.Apply(s.RequestBody.Payload, s.RequestBody.Replacements)
	}
	translate := func(r string) string { return translateInlineExpr(r, ids) }
	for _, e := range expr.UnsupportedInline(model.InlineValues(s, effective), translate) {
		fmt.Fprintf(b, "  // unsupported expression (not translated): %s\n", e)
	}

	op, method, url, ok := resolveRequestLine(s, sources, ids)
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
	url += queryString(s.Parameters, ids)
	if qs := querystringValue(s.Parameters, ids); qs != "" {
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
	bodyArg := writeBody(b, s.StepID, s.RequestBody, effective, unresolved, ct, ctKnown, ids)
	reqParams := "{ headers: " + headersObject(s.Parameters, ct, ctKnown, ids)
	if c := cookiesObject(s.Parameters, ids); c != "" {
		reqParams += ", cookies: " + c
	}
	reqParams += " }"
	fmt.Fprintf(b, "  const %s = http.request('%s', `%s`, %s, %s);\n",
		resVar, method, url, bodyArg, reqParams)

	writeChecks(b, resVar, s.StepID, s.SuccessCriteria)
	writeCaptures(b, resVar, s.StepID, s.Outputs, ids)
}

// writeBody declares the request body constant when the step has one and
// returns the argument to pass as the http.request body (the constant
// name for a raw string, JSON.stringify(...) for a structured payload,
// or "null" when there is no body). Runtime expressions inside the
// payload are translated to the JS identifiers holding their values.
func writeBody(b *strings.Builder, stepID string, body *model.RequestBody, effective any, unresolved []string, ct string, ctKnown bool, ids *identTable) string {
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
		value, _ := jsBodyValue(s, ids) // a string value never errors
		fmt.Fprintf(b, "  const %s = %s;\n", name, value)
		return name
	}
	if raw, err := jsBodyValue(effective, ids); err == nil {
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
func jsBodyValue(v any, ids *identTable) (string, error) {
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
	swapped := swapBodyExprs(v, ids, base, &idents)
	raw, err := jsonMarshal(swapped, "  ", "  ")
	if err != nil {
		return "", err
	}
	for i, ident := range idents {
		raw = strings.Replace(raw, `"`+bodyExprSentinel(base, i)+`"`, ident, 1)
	}
	return raw, nil
}

func swapBodyExprs(v any, ids *identTable, base string, repls *[]string) any {
	switch t := v.(type) {
	case string:
		if jsExpr, isExpr := exprIdent(t, ids); isExpr {
			*repls = append(*repls, jsExpr)
			return bodyExprSentinel(base, len(*repls)-1)
		}
		if lit, ok := jsTemplateLiteral(t, ids); ok {
			*repls = append(*repls, lit)
			return bodyExprSentinel(base, len(*repls)-1)
		}
		return t
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = swapBodyExprs(val, ids, base, repls)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = swapBodyExprs(val, ids, base, repls)
		}
		return out
	default:
		return v
	}
}

// jsTemplateLiteral renders a string carrying embedded {$expr}
// occurrences as a JS template literal (`Total: ${total}`). Only
// expressions resolving to a declared identifier are interpolated;
// ok is false when none does.
func jsTemplateLiteral(s string, ids *identTable) (string, bool) {
	locs := expr.EmbeddedRe.FindAllStringSubmatchIndex(s, -1)
	if len(locs) == 0 {
		return "", false
	}
	var b strings.Builder
	b.WriteString("`")
	last := 0
	interpolated := false
	for _, loc := range locs {
		jsExpr, isExpr := exprIdent(s[loc[2]:loc[3]], ids)
		if !isExpr {
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
func writeCaptures(b *strings.Builder, resVar, stepID string, outs []model.OutputEntry, ids *identTable) {
	for _, o := range outs {
		name := ids.declare(stepOutputKey(stepID, o.Name), jsIdent(stepID)+"_"+jsIdent(o.Name))
		fmt.Fprintf(b, "  const %s = %s;\n", name, translateCaptureExpr(resVar, o.Expression))
	}
}

// resolveRequestLine returns the HTTP method and URL template (a JS
// backtick-literal body, BASE_URL-prefixed) for the step, or ok=false
// when its operation reference could not be resolved.
func resolveRequestLine(s model.Step, sources map[string]*oasresolver.Source, ids *identTable) (op oasresolver.Operation, method, url string, ok bool) {
	op, ok = oasresolver.ResolveStepOperation(s, sources)
	if !ok {
		return oasresolver.Operation{}, "", "", false
	}
	// Always BASE_URL, never op.BaseURL: requests stay environment-agnostic.
	return op, op.Method, "${BASE_URL}" + substitutePathParams(op.Path, s.Parameters, ids), true
}

func substitutePathParams(path string, params []model.Parameter, ids *identTable) string {
	for _, p := range params {
		if p.In != "path" {
			continue
		}
		path = strings.ReplaceAll(path, "{"+p.Name+"}", urlValue(p.Value, ids))
	}
	return path
}

// queryString builds the URL query suffix (?a=1&b=2) from the step's
// query parameters, or "" when there are none.
func queryString(params []model.Parameter, ids *identTable) string {
	var parts []string
	for _, p := range params {
		if p.In != "query" {
			continue
		}
		parts = append(parts, p.Name+"="+urlValue(p.Value, ids))
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
func headersObject(params []model.Parameter, contentType string, ctKnown bool, ids *identTable) string {
	var entries []string
	hasCT := false
	for _, p := range params {
		if p.In != "header" {
			continue
		}
		if strings.EqualFold(p.Name, "content-type") {
			hasCT = true
		}
		entries = append(entries, fmt.Sprintf("%s: %s", jsString(p.Name), headerValue(p.Value, ids)))
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
func cookiesObject(params []model.Parameter, ids *identTable) string {
	var entries []string
	for _, p := range params {
		if p.In != "cookie" {
			continue
		}
		entries = append(entries, fmt.Sprintf("%s: %s", jsString(p.Name), headerValue(p.Value, ids)))
	}
	if len(entries) == 0 {
		return ""
	}
	return "{ " + strings.Join(entries, ", ") + " }"
}

// querystringValue returns the rendered whole-query-string parameter
// for interpolation inside the URL literal, or "".
func querystringValue(params []model.Parameter, ids *identTable) string {
	for _, p := range params {
		if p.In == "querystring" {
			return urlValue(p.Value, ids)
		}
	}
	return ""
}

// urlValue renders a path or query parameter for interpolation inside a
// backtick URL literal: declared runtime expressions become
// ${identifier} (whole-string or embedded {$expr}), anything else is
// inlined verbatim.
func urlValue(v any, ids *identTable) string {
	if s, ok := v.(string); ok {
		if jsExpr, isExpr := exprIdent(s, ids); isExpr {
			return "${" + jsExpr + "}"
		}
		// The value lands inside the request's backtick URL literal, so
		// literal text must be escaped; the {$expr} pattern survives the
		// escaper untouched.
		return expr.EmbeddedRe.ReplaceAllStringFunc(jsTemplateEscaper.Replace(s), func(m string) string {
			if jsExpr, isExpr := exprIdent(m[1:len(m)-1], ids); isExpr {
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
func headerValue(v any, ids *identTable) string {
	if s, ok := v.(string); ok {
		if jsExpr, isExpr := exprIdent(s, ids); isExpr {
			return jsExpr
		}
		if lit, ok := jsTemplateLiteral(s, ids); ok {
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

// exprIdent reports whether s is an inline Arazzo runtime expression
// whose name is declared and, if so, the JS expression it maps to.
func exprIdent(s string, ids *identTable) (string, bool) {
	out := translateInlineExpr(s, ids)
	return out, out != s
}

// translateInlineExpr maps an inline runtime expression to the JS
// expression reading its value ($inputs.foo -> foo, $steps.s.outputs.o
// -> s_o); an unrecognised form, or a name with no declared identifier
// (undeclared input, output of a step that produces no captures), is
// returned unchanged so it stays visible in the script. A
// #/<json-pointer> suffix becomes a bracket chain on the parsed value
// (arrays accept string indices in JS, so every segment is emitted as a
// string key).
func translateInlineExpr(s string, ids *identTable) string {
	e := expr.Parse(s)
	var key string
	switch e.Kind {
	case expr.KindInput:
		key = inputKey(e.Name)
	case expr.KindStepOutput:
		key = stepOutputKey(e.Name, e.OutputName)
	default:
		return s
	}
	base, declared := ids.lookup(key)
	if !declared {
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
