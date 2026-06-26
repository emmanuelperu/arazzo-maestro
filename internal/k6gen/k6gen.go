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
// ("$sourceDescriptions.<name>.<id>") works with any number of sources.
// Unresolvable steps emit a placeholder URL and a comment naming the id
// so the script stays valid JavaScript a human can patch.
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
	"regexp"
	"sort"
	"strings"

	"github.com/emmanuelperu/arazzo-maestro/internal/expr"
	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
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
	writeInputs(&b, wf.Inputs)
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
			fmt.Fprintf(b, "//   - %s (%s)\n", in.Name, in.Type)
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
// it matches what translateInlineExpr emits for $inputs.<name>.
func writeInputs(b *strings.Builder, inputs []model.InputProperty) {
	if len(inputs) == 0 {
		return
	}
	for _, in := range inputs {
		fmt.Fprintf(b, "const %s = __ENV[%s] || %s;\n", jsIdent(in.Name), jsString(in.Name), jsDefault(in.Default))
	}
	b.WriteString("\n")
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

	for _, e := range unsupportedInlineExprs(s) {
		fmt.Fprintf(b, "  // unsupported expression (not translated): %s\n", e)
	}

	method, url, ok := resolveRequestLine(s.OperationID, s.Parameters, sources, declared)
	if !ok {
		fmt.Fprintf(b, "  // unresolved operationId: %s\n", s.OperationID)
		method = "GET"
		url = "${BASE_URL}/__unresolved__/" + s.OperationID
	}
	url += queryString(s.Parameters, declared)
	if qs := querystringValue(s.Parameters, declared); qs != "" {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url += sep + qs
	}

	resVar := jsIdent(s.StepID) + "Res"
	bodyArg := writeBody(b, s.StepID, s.RequestBody, declared)
	reqParams := "{ headers: " + headersObject(s.Parameters, declared)
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
func writeBody(b *strings.Builder, stepID string, body *model.RequestBody, declared map[string]bool) string {
	if body == nil {
		return "null"
	}
	fmt.Fprintf(b, "  // requestBody content-type: %s\n", body.ContentType)
	name := jsIdent(stepID) + "Body"
	if s, ok := body.Payload.(string); ok {
		value, _ := jsBodyValue(s, declared) // a string value never errors
		fmt.Fprintf(b, "  const %s = %s;\n", name, value)
		return name
	}
	if raw, err := jsBodyValue(body.Payload, declared); err == nil {
		fmt.Fprintf(b, "  const %s = %s;\n", name, raw)
		return "JSON.stringify(" + name + ")"
	}
	fmt.Fprintf(b, "  const %s = %s;\n", name, jsString(fmt.Sprintf("%v", body.Payload)))
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
		if ident, isExpr := exprIdent(t); isExpr && declared[ident] {
			*repls = append(*repls, ident)
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
		ident, isExpr := exprIdent(expr)
		if !isExpr || !declared[ident] {
			continue
		}
		b.WriteString(jsTemplateEscaper.Replace(s[last:loc[0]]))
		b.WriteString("${" + ident + "}")
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
		if pred, ok := translateCondition(c.Condition); ok {
			predicates = append(predicates, fmt.Sprintf("    %s: (r) => %s,",
				jsString(stepID+": "+c.Condition), pred))
		} else {
			unsupported = append(unsupported, c.Condition)
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
// when the operationId could not be resolved.
func resolveRequestLine(operationID string, params []model.Parameter, sources map[string]*oasresolver.Source, declared map[string]bool) (method, url string, ok bool) {
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
	// Always BASE_URL, never op.BaseURL: requests stay environment-agnostic.
	return op.Method, "${BASE_URL}" + substitutePathParams(op.Path, params, declared), true
}

// defaultBaseURL returns the OpenAPI servers URL backing the workflow's
// first resolvable step, or "" when none resolves. Documentation only:
// requests always use the BASE_URL constant.
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

// parseOpRef recognises the short ("createOrder") and qualified
// ("$sourceDescriptions.<name>.<id>") forms of operationId. Short form
// resolves only when exactly one source is configured; the linter
// enforces the qualified form upstream when several sources exist.
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
// literal for the request params. An empty set yields "{}".
func headersObject(params []model.Parameter, declared map[string]bool) string {
	var entries []string
	for _, p := range params {
		if p.In != "header" {
			continue
		}
		entries = append(entries, fmt.Sprintf("%s: %s", jsString(p.Name), headerValue(p.Value, declared)))
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
		if ident, isExpr := exprIdent(s); isExpr && declared[ident] {
			return "${" + ident + "}"
		}
		// The value lands inside the request's backtick URL literal, so
		// literal text must be escaped; the {$expr} pattern survives the
		// escaper untouched.
		return embeddedExprRe.ReplaceAllStringFunc(jsTemplateEscaper.Replace(s), func(m string) string {
			if ident, isExpr := exprIdent(m[1 : len(m)-1]); isExpr && declared[ident] {
				return "${" + ident + "}"
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
		if ident, isExpr := exprIdent(s); isExpr && declared[ident] {
			return ident
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
		return "null; // unsupported: " + s
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

// unsupportedInlineExprs returns the runtime expressions used in the
// step's inline values (parameters and request body) whose form the
// generator cannot translate to an identifier. Without this they would
// be emitted as literal strings with no signal; writeStep flags each as
// a comment, mirroring the marker the capture side already produces.
// A recognised form that merely references an undeclared step is not
// flagged here: that is a linter concern, not an untranslatable form.
func unsupportedInlineExprs(s model.Step) []string {
	var refs []string
	for _, p := range s.Parameters {
		refs = append(refs, expr.CollectRefs(p.Value)...)
	}
	if s.RequestBody != nil {
		refs = append(refs, expr.CollectRefs(s.RequestBody.Payload)...)
	}
	var out []string
	seen := make(map[string]bool)
	for _, r := range refs {
		if seen[r] || translateInlineExpr(r) != r {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// translateInlineExpr maps an inline runtime expression to the JS
// identifier holding its value ($inputs.foo -> foo, $steps.s.outputs.o
// -> s_o); anything else is returned unchanged.
func translateInlineExpr(s string) string {
	switch e := expr.Parse(s); e.Kind {
	case expr.KindInput:
		return jsIdent(e.Name)
	case expr.KindStepOutput:
		return jsIdent(e.Name) + "_" + jsIdent(e.OutputName)
	default:
		return s
	}
}

// jsonPointerToGJSON converts the body of a JSON Pointer (after '#/')
// to the gjson selector k6's Response.json() expects.
func jsonPointerToGJSON(ptr string) string {
	segs := strings.Split(ptr, "/")
	for i, seg := range segs {
		segs[i] = gjsonEscaper.Replace(unescapeJSONPointer(seg))
	}
	return strings.Join(segs, ".")
}

// gjsonEscaper protects every character that gjson treats as syntax
// inside a path segment, including its own escape character.
var gjsonEscaper = strings.NewReplacer(
	`\`, `\\`, ".", `\.`, "*", `\*`, "?", `\?`, "|", `\|`, "#", `\#`,
)

// unescapeJSONPointer decodes the RFC 6901 escape sequences inside a
// pointer segment: ~1 is '/', ~0 is '~'. ~1 must be decoded first so
// that ~01 yields the literal ~1.
func unescapeJSONPointer(seg string) string {
	return strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
}

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
