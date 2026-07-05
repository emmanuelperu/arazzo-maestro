// Package model defines the typed representation of the subset of Arazzo
// the visualiser actually renders. Unknown YAML fields are ignored, the
// validator is supposed to run upstream.
package model

// InputProperty is a single property listed under a workflow's `inputs` block.
type InputProperty struct {
	Name    string
	Type    string
	Default any
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

// SuccessCriterion is one assertion checked after a step.
type SuccessCriterion struct {
	Condition string
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
// modelled yet: workflow inputs are only read one property level deep
// (issue #57 tracks the full JSON Schema depth).
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
