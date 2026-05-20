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

// Parameter is one entry of a step's `parameters` array.
type Parameter struct {
	Name  string
	In    string
	Value any
}

// RequestBody mirrors the optional `requestBody` block of a step.
type RequestBody struct {
	ContentType string
	Payload     any
}

// SuccessCriterion is one assertion checked after a step.
type SuccessCriterion struct {
	Condition string
}

// Step is one entry of a workflow's `steps` array.
type Step struct {
	StepID          string
	Description     string
	OperationID     string
	Parameters      []Parameter
	RequestBody     *RequestBody
	SuccessCriteria []SuccessCriterion
	Outputs         []OutputEntry
	OnSuccess       []SuccessAction
	OnFailure       []FailureAction
}

// SuccessAction is one entry of a step's `onSuccess` array. Per the
// Arazzo spec, `Type` is one of "end" or "goto".
type SuccessAction struct {
	Name       string
	Type       string // "end" | "goto"
	StepID     string // when Type == "goto" within the current workflow
	WorkflowID string // when Type == "goto" to a different workflow
	Criteria   []SuccessCriterion
}

// FailureAction is one entry of a step's `onFailure` array. Per the
// Arazzo spec, `Type` is one of "end", "goto", or "retry".
type FailureAction struct {
	Name       string
	Type       string // "end" | "goto" | "retry"
	StepID     string
	WorkflowID string
	RetryAfter int // milliseconds, only when Type == "retry"
	RetryLimit int // count, only when Type == "retry"
	Criteria   []SuccessCriterion
}

// SourceDescription is one entry of the top-level `sourceDescriptions` array.
type SourceDescription struct {
	Name string
	URL  string
	Type string
}

// Workflow is one entry of the top-level `workflows` array.
type Workflow struct {
	WorkflowID  string
	Summary     string
	Description string
	Inputs      []InputProperty
	Steps       []Step
	Outputs     []OutputEntry
}

// OutputEntry preserves the ordered key/value pairs of an `outputs` block.
// YAML maps don't keep insertion order in Go's map[string]any, but the
// rendered HTML should display outputs in their declared order.
type OutputEntry struct {
	Name       string
	Expression string
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
}
