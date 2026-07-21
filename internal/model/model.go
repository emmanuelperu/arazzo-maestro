// Package model defines the typed representation of the subset of Arazzo
// the visualiser actually renders. Unknown YAML fields are ignored, the
// validator is supposed to run upstream.
package model

import "strings"

// InputProperty is a single property listed under a workflow's `inputs`
// block. Nested object properties are flattened to dotted names
// ("user.name") for declaration purposes; arrays, enums and $ref stay
// out of the model (the schema pass validates them).
type InputProperty struct {
	Name     string
	Type     string
	Default  any
	Required bool
}

// Parameter is one entry of a step's `parameters` array. Reference is
// set when the entry was declared as a Reusable Object
// (`{reference: $components.parameters.<name>}`); the other fields then
// hold the resolved component (inlined at parse time), or stay zero
// when the reference does not resolve (the linter reports it).
// Inherited marks a copy merged in from the workflow-level `parameters`
// defaults, which apply to every step unless overridden.
type Parameter struct {
	Name      string
	In        string
	Value     any
	Reference string
	Inherited bool
}

// RequestBody mirrors the optional `requestBody` block of a step.
type RequestBody struct {
	ContentType  string
	Payload      any
	Replacements []Replacement
}

// Replacement is one entry of a request body's `replacements` array (a
// Payload Replacement Object): the value at the `target` JSON pointer is
// set to `Value` before the body is sent. Both fields are required.
type Replacement struct {
	Target string
	Value  any
}

// SuccessCriterion is one assertion checked after a step. Type is one
// of the spec's simple/regex/jsonpath/xpath (empty means simple);
// TypeVersion carries the Criterion Expression Type Object version
// (written flat next to `type` per the official schema, or nested
// under it per the spec prose). Context is the runtime expression the
// condition is applied to, required by the spec whenever Type is set.
type SuccessCriterion struct {
	Condition   string
	Context     string
	Type        string
	TypeVersion string
}

// Describe returns the criterion for a generated comment: the condition
// prefixed with its declared type and version, followed by its context.
func (c SuccessCriterion) Describe() string {
	out := c.Condition
	if c.Type != "" || c.TypeVersion != "" {
		typ := c.Type
		if c.TypeVersion != "" {
			typ = strings.TrimSpace(typ + " " + c.TypeVersion)
		}
		out = "[" + typ + "] " + out
	}
	if c.Context != "" {
		out += "  (context: " + c.Context + ")"
	}
	return out
}

// IsSimple reports whether the criterion is in the spec's simple
// condition mini-language, the only one the generators translate.
func (c SuccessCriterion) IsSimple() bool {
	return (c.Type == "" && c.TypeVersion == "") || c.Type == "simple"
}

// TypeLabel returns the input's type plus its required marker for
// generated header comments.
func (in InputProperty) TypeLabel() string {
	if in.Required {
		return in.Type + ", required"
	}
	return in.Type
}

// Step is one entry of a workflow's `steps` array. OperationID,
// OperationPath and WorkflowID are mutually exclusive per the Arazzo
// step oneOf; the schema pass reports violations, the model just holds
// whichever was declared.
type Step struct {
	StepID          string
	Description     string
	OperationID     string
	OperationPath   string
	WorkflowID      string
	Parameters      []Parameter
	RequestBody     *RequestBody
	SuccessCriteria []SuccessCriterion
	Outputs         []OutputEntry
	OnSuccess       []SuccessAction
	OnFailure       []FailureAction
}

// SuccessAction is one entry of a step's `onSuccess` array. Per the
// Arazzo spec, `Type` is one of "end" or "goto". Reference is set when
// the entry was declared as a Reusable Object; see Parameter.Reference.
type SuccessAction struct {
	Name       string
	Type       string // "end" | "goto"
	StepID     string // when Type == "goto" within the current workflow
	WorkflowID string // when Type == "goto" to a different workflow
	Criteria   []SuccessCriterion
	Reference  string
	Inherited  bool // merged in from the workflow-level defaults
}

// FailureAction is one entry of a step's `onFailure` array. Per the
// Arazzo spec, `Type` is one of "end", "goto", or "retry". Reference is
// set when the entry was declared as a Reusable Object; see
// Parameter.Reference.
type FailureAction struct {
	Name          string
	Type          string // "end" | "goto" | "retry"
	StepID        string
	WorkflowID    string
	RetryAfter    float64 // seconds (spec: non-negative decimal), only when Type == "retry"
	RetryLimit    int     // count, only when Type == "retry"
	RetryLimitSet bool    // distinguishes an explicit 0 from the spec default (a single retry)
	Criteria      []SuccessCriterion
	Reference     string
	Inherited     bool // merged in from the workflow-level defaults
}

// SourceDescription is one entry of the top-level `sourceDescriptions` array.
type SourceDescription struct {
	Name string
	URL  string
	Type string
}

// Workflow is one entry of the top-level `workflows` array.
type Workflow struct {
	WorkflowID     string
	Summary        string
	Description    string
	DependsOn      []string // workflowIds that must complete before this workflow runs
	Inputs         []InputProperty
	Parameters     []Parameter     // workflow-level defaults, also merged into every step
	SuccessActions []SuccessAction // workflow-level defaults, also merged into every step
	FailureActions []FailureAction // workflow-level defaults, also merged into every step
	Steps          []Step
	Outputs        []OutputEntry
}

// OutputEntry preserves the ordered key/value pairs of an `outputs` block.
// YAML maps don't keep insertion order in Go's map[string]any, but the
// rendered HTML should display outputs in their declared order.
type OutputEntry struct {
	Name       string
	Expression string
}

// Components holds the document's reusable objects (Components Object).
// Reusable `inputs` schemas are accepted by the schema pass but not
// modelled: workflow inputs only keep per-property type/default/required
// (arrays, enums and $ref stay schema-only).
type Components struct {
	Parameters     map[string]Parameter
	SuccessActions map[string]SuccessAction
	FailureActions map[string]FailureAction
}

// ArazzoDocument is the root of a parsed Arazzo file.
type ArazzoDocument struct {
	Arazzo             string
	Title              string
	Summary            string
	Description        string
	Version            string
	SourceDescriptions []SourceDescription
	Workflows          []Workflow
	Components         Components
}

// ResolvedParameters filters out unresolved Reusable Object entries
// (Reference != ""): they carry no usable name/in/value and must not
// reach a request, so every consumer applies the same rule.
func ResolvedParameters(params []Parameter) []Parameter {
	out := make([]Parameter, 0, len(params))
	for _, p := range params {
		if p.Reference == "" {
			out = append(out, p)
		}
	}
	return out
}

// OwnParameters filters out the entries merged in from the
// workflow-level defaults, keeping only what the step declares itself.
func OwnParameters(params []Parameter) []Parameter {
	out := make([]Parameter, 0, len(params))
	for _, p := range params {
		if !p.Inherited {
			out = append(out, p)
		}
	}
	return out
}

// OutputNames joins the declared output names for a comment or message.
func OutputNames(outs []OutputEntry) string {
	names := make([]string, len(outs))
	for i, o := range outs {
		names[i] = o.Name
	}
	return strings.Join(names, ", ")
}

// InlineValues lists the values a step emits inline (parameter values
// and the request body after replacements), so a generator's
// unsupported-expression scan sees exactly what gets serialised.
func InlineValues(s Step, effectiveBody any) []any {
	values := make([]any, 0, len(s.Parameters)+1)
	for _, p := range s.Parameters {
		values = append(values, p.Value)
	}
	if s.RequestBody != nil {
		values = append(values, effectiveBody)
	}
	return values
}
