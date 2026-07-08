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
//	parameters in=cookie      -> [Cookies]
//	parameters in=querystring -> appended to the request URL
//
// Operation resolution goes through the oasresolver package: callers
// pass a map of source-description name to *oasresolver.Source. Short
// operationId forms ("listUsers") resolve when exactly one source is
// configured; the qualified form ("$sourceDescriptions.<name>.<id>")
// works with any number of sources, and operationPath references
// resolve their JSON pointer against the named source. Steps whose
// reference cannot be resolved emit a placeholder request line and a
// comment naming the unresolved reference, so the output stays valid
// Hurl that a human can patch. Steps that invoke another workflow
// (workflowId) emit an explicit not-supported comment and no request:
// a nested workflow is not one HTTP call, and a placeholder request
// would fail against the real endpoint.
//
// Arazzo runtime expressions are translated: $inputs.foo becomes
// {{foo}}, $steps.s.outputs.o becomes {{s_o}}, $response.body#/x/y
// becomes `jsonpath "$.x.y"`, and the spec's embedded {$expr} form is
// interpolated in place. A #/<json-pointer> sub-access on a step
// output becomes a derived capture at the producing step (the pointer
// folded into the source jsonpath); on an input it stays untranslated
// and flagged, because Hurl offers no render-time sub-access on a
// variable (member access on a structured value is unrenderable and
// placeholder filters are silently ignored, both verified against
// hurl 8.0.1). Unknown forms pass through unchanged.
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
	neturl "net/url"
	"regexp"
	"strings"

	"github.com/emmanuelperu/arazzo-maestro/internal/expr"
	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
	"github.com/emmanuelperu/arazzo-maestro/internal/payload"
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
	tr := newTranslator(wf, sources)
	for i, step := range wf.Steps {
		if i > 0 {
			b.WriteString("\n")
		}
		tr.writeStep(&b, step)
	}
	return b.String(), nil
}

// derivedCapture is an extra capture emitted at the step producing an
// output that a later reference sub-accesses with a #/<json-pointer>
// suffix: the pointer is folded into the source jsonpath, because Hurl
// placeholders cannot navigate a structured capture at render time
// (member access and filters on an Object value fail, verified against
// hurl 8.0.1).
type derivedCapture struct {
	name  string
	query string
}

// translator carries the per-Generate state the expression translation
// needs: the loaded sources, the unquoted-input set, and the derived
// captures planned by the pre-scan.
type translator struct {
	sources       map[string]*oasresolver.Source
	unquoted      map[string]bool
	derived       map[string]string // trimmed raw expression -> derived capture name
	derivedByStep map[string][]derivedCapture
}

// newTranslator pre-scans the workflow for $steps output references
// carrying a #/<json-pointer> suffix and plans one derived capture per
// distinct (output, pointer) pair at the producing step.
func newTranslator(wf model.Workflow, sources map[string]*oasresolver.Source) *translator {
	tr := &translator{
		sources:       sources,
		unquoted:      nonStringInputs(wf.Inputs),
		derived:       make(map[string]string),
		derivedByStep: make(map[string][]derivedCapture),
	}

	// Outputs per step, excluding workflow-invoking steps: those emit no
	// request, so their outputs are never captured and references to
	// them must stay visibly untranslated.
	outputs := make(map[string]map[string]string, len(wf.Steps))
	taken := make(map[string]bool)
	for _, step := range wf.Steps {
		if step.WorkflowID != "" {
			continue
		}
		outs := make(map[string]string, len(step.Outputs))
		for _, o := range step.Outputs {
			outs[o.Name] = o.Expression
			taken[step.StepID+"_"+o.Name] = true
		}
		outputs[step.StepID] = outs
	}

	for _, step := range wf.Steps {
		if step.WorkflowID != "" {
			continue
		}
		// Match what writeStep serialises: unresolved reusable parameters
		// are dropped there, so their values must not plan captures.
		step.Parameters = resolvedParameters(step.Parameters)
		var effective any
		if step.RequestBody != nil {
			effective, _ = payload.Apply(step.RequestBody.Payload, step.RequestBody.Replacements)
		}
		for _, value := range inlineValues(step, effective) {
			for _, ref := range expr.CollectRefs(value) {
				tr.planDerivedCapture(ref, outputs, taken)
			}
		}
	}
	return tr
}

// planDerivedCapture registers a derived capture for one referenced
// expression when it is a pointer sub-access on a translatable step
// output; anything else is left for the unsupported-expression scan.
func (tr *translator) planDerivedCapture(ref string, outputs map[string]map[string]string, taken map[string]bool) {
	key := strings.TrimSpace(ref)
	if _, done := tr.derived[key]; done {
		return
	}
	e := expr.Parse(ref)
	if e.Kind != expr.KindStepOutput || !e.HasPointer {
		return
	}
	if !hurlVarSafe(e.Name) || !hurlVarSafe(e.OutputName) {
		return
	}
	srcExpr, declared := outputs[e.Name][e.OutputName]
	if !declared {
		return
	}
	// Fold the reference pointer into the source capture: only a
	// $response.body output has a JSON document to point into.
	src := expr.Parse(srcExpr)
	if src.Kind != expr.KindResponseBody {
		return
	}
	pointer := e.Pointer
	if src.HasPointer {
		pointer = src.Pointer + "/" + e.Pointer
	}
	path, ok := jsonPointerToJSONPath(pointer)
	if !ok {
		return
	}
	base := e.Name + "_" + e.OutputName + "_" + sanitizeVar(e.Pointer)
	name := base
	for i := 2; taken[name]; i++ {
		name = fmt.Sprintf("%s_%d", base, i)
	}
	taken[name] = true
	tr.derived[key] = name
	tr.derivedByStep[e.Name] = append(tr.derivedByStep[e.Name], derivedCapture{
		name:  name,
		query: `jsonpath "` + path + `"`,
	})
}

// sanitizeVar maps a JSON-pointer body to a Hurl-variable-safe suffix.
func sanitizeVar(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
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
		dotted := false
		for _, in := range wf.Inputs {
			fmt.Fprintf(b, "#   - %s (%s)\n", in.Name, in.TypeLabel())
			dotted = dotted || strings.ContainsRune(in.Name, '.')
		}
		if dotted {
			b.WriteString("#   note: dotted input names cannot become Hurl variables; their references stay literal\n")
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
		op, ok := oasresolver.ResolveStepOperation(s, sources)
		if ok && op.BaseURL != "" {
			return op.BaseURL
		}
	}
	return ""
}

func (tr *translator) writeStep(b *strings.Builder, s model.Step) {
	fmt.Fprintf(b, "# Step: %s\n", s.StepID)
	if s.Description != "" {
		for _, line := range strings.Split(strings.TrimSpace(s.Description), "\n") {
			fmt.Fprintf(b, "# %s\n", line)
		}
	}
	if s.WorkflowID != "" {
		fmt.Fprintf(b, "# not supported: this step invokes workflow %q (workflowId); no request generated\n", s.WorkflowID)
		if len(s.Parameters) > 0 {
			b.WriteString("# warning: parameters are not forwarded to the invoked workflow\n")
		}
		if len(s.Outputs) > 0 {
			fmt.Fprintf(b, "# warning: outputs %s are not captured; later references to them stay unresolved\n", outputNames(s.Outputs))
		}
		return
	}

	// Apply payload replacements once, up front: the unsupported-expression
	// scan and the serialised body must agree on the same (post-replacement)
	// payload.
	var effective any
	var unresolved []string
	if s.RequestBody != nil {
		effective, unresolved = payload.Apply(s.RequestBody.Payload, s.RequestBody.Replacements)
	}
	for _, e := range expr.UnsupportedInline(inlineValues(s, effective), tr.translateInlineExpr) {
		fmt.Fprintf(b, "# unsupported expression (not translated): %s\n", e)
	}
	for _, p := range s.Parameters {
		if p.Reference != "" {
			fmt.Fprintf(b, "# unresolved component reference (parameter dropped): %s\n", p.Reference)
		}
	}
	// s is a copy: dropping unresolved entries here keeps every writer
	// below (headers, query, path, cookies, body scan) consistent with
	// the dropped-comment above.
	s.Parameters = resolvedParameters(s.Parameters)

	op, method, url, ok := tr.resolveRequestLine(s)
	if !ok {
		method = "GET"
		if s.OperationPath != "" {
			fmt.Fprintf(b, "# unresolved operationPath: %s\n", s.OperationPath)
			// The raw reference carries JSON-pointer escapes and braces, so
			// the placeholder URL names the step instead; escaping keeps the
			// request line one URL token whatever the stepId contains.
			url = "{{baseUrl}}/__unresolved__/" + neturl.PathEscape(s.StepID)
		} else {
			fmt.Fprintf(b, "# unresolved operationId: %s\n", s.OperationID)
			url = "{{baseUrl}}/__unresolved__/" + s.OperationID
		}
	}
	if qs := tr.querystringValue(s.Parameters); qs != "" {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url += sep + qs
	}
	fmt.Fprintf(b, "%s %s\n", method, url)

	// Effective content type for the body: explicit Arazzo value, else the
	// targeted operation's declared type (Arazzo defers to it when omitted).
	ct, ctKnown := "", false
	if s.RequestBody != nil {
		ct, ctKnown = oasresolver.EffectiveContentType(s.RequestBody.ContentType, op.RequestContentTypes())
	}

	tr.writeHeaders(b, s.Parameters)
	if ctKnown && !hasHeaderParam(s.Parameters, "content-type") {
		fmt.Fprintf(b, "Content-Type: %s\n", ct)
	}
	tr.writeQuery(b, s.Parameters)
	tr.writeCookies(b, s.Parameters)
	tr.writeBody(b, s.RequestBody, effective, unresolved, ct, ctKnown)

	b.WriteString("\nHTTP *\n")
	writeAsserts(b, s.SuccessCriteria)
	tr.writeCaptures(b, s.StepID, s.Outputs)
}

func (tr *translator) writeHeaders(b *strings.Builder, params []model.Parameter) {
	for _, p := range params {
		if p.In != "header" {
			continue
		}
		fmt.Fprintf(b, "%s: %s\n", p.Name, tr.renderValue(p.Value))
	}
}

func (tr *translator) writeQuery(b *strings.Builder, params []model.Parameter) {
	first := true
	for _, p := range params {
		if p.In != "query" {
			continue
		}
		if first {
			b.WriteString("[QueryStringParams]\n")
			first = false
		}
		fmt.Fprintf(b, "%s: %s\n", p.Name, tr.renderValue(p.Value))
	}
}

func (tr *translator) writeCookies(b *strings.Builder, params []model.Parameter) {
	first := true
	for _, p := range params {
		if p.In != "cookie" {
			continue
		}
		if first {
			b.WriteString("[Cookies]\n")
			first = false
		}
		fmt.Fprintf(b, "%s: %s\n", p.Name, tr.renderValue(p.Value))
	}
}

// querystringValue returns the rendered value of the querystring
// parameter, the spec's whole-query-string location, or "".
func (tr *translator) querystringValue(params []model.Parameter) string {
	for _, p := range params {
		if p.In == "querystring" {
			return tr.renderValue(p.Value)
		}
	}
	return ""
}

// writeBody emits the body block from the already-replacement-applied
// payload (effective) and the unresolved replacement targets, both
// computed once by writeStep so the inline scan and the serialised body
// stay in sync.
func (tr *translator) writeBody(b *strings.Builder, body *model.RequestBody, effective any, unresolved []string, ct string, ctKnown bool) {
	if body == nil {
		return
	}
	if ctKnown {
		fmt.Fprintf(b, "# requestBody content-type: %s\n", ct)
	} else {
		b.WriteString("# requestBody content-type: unknown (omitted by Arazzo; the operation declares none or several non-JSON types)\n")
	}
	for _, r := range body.Replacements {
		fmt.Fprintf(b, "# replacement: %s = %s\n", r.Target, compactValue(r.Value))
	}
	for _, u := range unresolved {
		fmt.Fprintf(b, "# warning: replacement target %q did not resolve in the payload\n", u)
	}
	if payloadHasLiteralBraces(effective) {
		b.WriteString("# warning: literal '{{' in the body is interpreted by Hurl templating at run time\n")
	}
	fmt.Fprintf(b, "```\n%s\n```\n", tr.serialiseBody(effective, ct))
}

// inlineValues lists the values a step emits inline (parameter values and
// the request body after replacements) for the unsupported-expression
// scan, so the scan sees exactly what gets serialised.
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

// compactValue renders a replacement value for a comment: JSON when it
// marshals (so objects, arrays and quoted strings read naturally),
// otherwise Go's default formatting.
func compactValue(v any) string {
	if raw, err := json.Marshal(v); err == nil {
		return string(raw)
	}
	return fmt.Sprintf("%v", v)
}

// hasHeaderParam reports whether the step already declares a header
// parameter with this name (case-insensitive), so a derived Content-Type
// never duplicates an explicit one.
func hasHeaderParam(params []model.Parameter, name string) bool {
	for _, p := range params {
		if p.In == "header" && strings.EqualFold(p.Name, name) {
			return true
		}
	}
	return false
}

// payloadHasLiteralBraces reports whether any string in the payload
// contains '{{' before translation: Hurl has no escape for literal
// moustaches, so such data collides with templating at run time.
func payloadHasLiteralBraces(v any) bool {
	switch t := v.(type) {
	case string:
		return strings.Contains(t, "{{")
	case map[string]any:
		for _, val := range t {
			if payloadHasLiteralBraces(val) {
				return true
			}
		}
	case []any:
		for _, val := range t {
			if payloadHasLiteralBraces(val) {
				return true
			}
		}
	}
	return false
}

// serialiseBody turns the requestBody payload into the text of the Hurl
// body block, with runtime expressions translated to Hurl templates.
func (tr *translator) serialiseBody(payload any, ct string) string {
	if s, ok := payload.(string); ok {
		return tr.renderValue(s)
	}
	if strings.Contains(ct, "json") {
		if out, err := tr.jsonBodyWithTemplates(payload); err == nil {
			return out
		}
	}
	return fmt.Sprintf("%v", tr.translateBodyExprs(payload))
}

// jsonBodyWithTemplates marshals the payload with expressions swapped
// for sentinels absent from the payload itself, so a literal string can
// never be mistaken for a template.
func (tr *translator) jsonBodyWithTemplates(payload any) (string, error) {
	probe, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	base := "__arazzo_tpl_"
	for strings.Contains(string(probe), base) {
		base = "_" + base
	}
	var repls []string
	swapped := tr.swapBodyExprs(payload, base, &repls)
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

func (tr *translator) swapBodyExprs(v any, base string, repls *[]string) any {
	switch t := v.(type) {
	case string:
		tpl := tr.translateInlineExpr(t)
		if tpl == t {
			// Not a whole-string expression: translate the spec's
			// embedded {$expr} occurrences in place; the result stays
			// a plain string.
			out, _ := tr.translateEmbedded(t)
			return out
		}
		repl := `"` + tpl + `"`
		if e := strings.TrimSpace(t); strings.HasPrefix(e, "$inputs.") && tr.unquoted[strings.TrimPrefix(e, "$inputs.")] {
			repl = tpl
		}
		*repls = append(*repls, repl)
		return fmt.Sprintf("%s%d__", base, len(*repls)-1)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = tr.swapBodyExprs(val, base, repls)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = tr.swapBodyExprs(val, base, repls)
		}
		return out
	default:
		return v
	}
}

func (tr *translator) translateBodyExprs(v any) any {
	switch t := v.(type) {
	case string:
		return tr.renderValue(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = tr.translateBodyExprs(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = tr.translateBodyExprs(val)
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
		fmt.Fprintf(b, "# %s\n", c.Describe())
	}
}

// writeCaptures emits the step's outputs as a Hurl [Captures] block.
// Capture variables are namespaced by step id (<stepId>_<outputName>)
// so later steps can resolve them with the same translation that
// $steps.<stepId>.outputs.<outputName> uses inline.
func (tr *translator) writeCaptures(b *strings.Builder, stepID string, outs []model.OutputEntry) {
	derived := tr.derivedByStep[stepID]
	if len(outs) == 0 && len(derived) == 0 {
		return
	}
	b.WriteString("[Captures]\n")
	for _, o := range outs {
		fmt.Fprintf(b, "%s_%s: %s\n", stepID, o.Name, translateCaptureExpr(o.Expression))
	}
	// Derived captures serve later #/<json-pointer> sub-accesses on this
	// step's outputs (the pointer is folded into the source jsonpath).
	for _, d := range derived {
		fmt.Fprintf(b, "%s: %s\n", d.name, d.query)
	}
}

// resolveRequestLine returns the HTTP method and full URL for the
// step, or ok=false when its operation reference could not be resolved
// against the configured sources.
func (tr *translator) resolveRequestLine(s model.Step) (op oasresolver.Operation, method, url string, ok bool) {
	op, ok = oasresolver.ResolveStepOperation(s, tr.sources)
	if !ok {
		return oasresolver.Operation{}, "", "", false
	}
	// Always {{baseUrl}}, never op.BaseURL: requests stay environment-agnostic.
	path := tr.substitutePathParams(op.Path, s.Parameters)
	return op, op.Method, "{{baseUrl}}" + path, true
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

func (tr *translator) substitutePathParams(path string, params []model.Parameter) string {
	for _, p := range params {
		if p.In != "path" {
			continue
		}
		placeholder := "{" + p.Name + "}"
		path = strings.ReplaceAll(path, placeholder, tr.renderValue(p.Value))
	}
	return path
}

func (tr *translator) renderValue(v any) string {
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	if tpl := tr.translateInlineExpr(s); tpl != s {
		return tpl
	}
	out, _ := tr.translateEmbedded(s)
	return out
}

// embeddedExprRe matches the spec's embedded form: a runtime expression
// wrapped in {} curly braces inside a string value.
var embeddedExprRe = regexp.MustCompile(`\{(\$[^{}]+)\}`)

// translateEmbedded replaces every embedded {$expr} whose expression is
// recognised with its Hurl template; unrecognised ones pass through.
func (tr *translator) translateEmbedded(s string) (string, bool) {
	changed := false
	out := embeddedExprRe.ReplaceAllStringFunc(s, func(m string) string {
		expr := m[1 : len(m)-1]
		if tpl := tr.translateInlineExpr(expr); tpl != expr {
			changed = true
			return tpl
		}
		return m
	})
	return out, changed
}

// translateInlineExpr maps an inline runtime expression to a Hurl
// template ($inputs.foo -> {{foo}}, $steps.s.outputs.o -> {{s_o}});
// anything else is returned unchanged so the user can spot it. A
// #/<json-pointer> suffix on a step output resolves to the derived
// capture the pre-scan planned; on an input it is declined: Hurl has
// no render-time sub-access on a variable (member access on a
// structured value is unrenderable and placeholder filters are
// silently ignored, both verified against hurl 8.0.1).
func (tr *translator) translateInlineExpr(s string) string {
	switch e := expr.Parse(s); e.Kind {
	case expr.KindInput:
		if hurlVarSafe(e.Name) && !e.HasPointer {
			return "{{" + e.Name + "}}"
		}
		return s
	case expr.KindStepOutput:
		if e.HasPointer {
			if name, ok := tr.derived[strings.TrimSpace(s)]; ok {
				return "{{" + name + "}}"
			}
			return s
		}
		if hurlVarSafe(e.Name) && hurlVarSafe(e.OutputName) {
			return "{{" + e.Name + "_" + e.OutputName + "}}"
		}
		return s
	default:
		return s
	}
}

// hurlVarSafe reports whether an Arazzo name maps to a usable Hurl
// variable. Hurl reads the '.' in {{a.b}} as member access on a variable
// named "a", not as a variable literally named "a.b" (verified against
// hurl 8.0.1), so a dotted name cannot become a Hurl variable. Declining
// it leaves the expression for the inline scan to flag rather than
// emitting a template Hurl would resolve to the wrong variable.
func hurlVarSafe(name string) bool {
	return !strings.ContainsRune(name, '.')
}

// translateCaptureExpr maps an Arazzo output expression to a Hurl
// capture right-hand side. Recognised forms:
//
//	$response.body        ->  jsonpath "$"
//	$response.body#/path  ->  jsonpath "$.path"
//	$statusCode           ->  status
//
// Anything else is returned as a Hurl comment so the user spots that
// the capture was not understood.
func translateCaptureExpr(s string) string {
	switch e := expr.Parse(s); e.Kind {
	case expr.KindResponseBody:
		if !e.HasPointer {
			return `jsonpath "$"`
		}
		if path, ok := jsonPointerToJSONPath(e.Pointer); ok {
			return `jsonpath "` + path + `"`
		}
		return "# unsupported: " + s
	case expr.KindStatusCode:
		return "status"
	default:
		return "# unsupported: " + s
	}
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
		seg = expr.UnescapeJSONPointer(seg)
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
