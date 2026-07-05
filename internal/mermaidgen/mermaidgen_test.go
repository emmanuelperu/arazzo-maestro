package mermaidgen

import (
	"strings"
	"testing"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
)

func TestGenerateSequentialHappyPath(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "checkout",
		Steps: []model.Step{
			{StepID: "list", OperationID: "listProducts"},
			{StepID: "pay", OperationID: "pay"},
		},
	}
	out := Generate(wf)

	for _, want := range []string{
		"flowchart TD",
		`s0["01 list<br/>listProducts"]`,
		`s1["02 pay<br/>pay"]`,
		"wfStart([Start])",
		"wfEnd([End])",
		"wfStart --> s0",
		"s0 --> s1",
		"s1 --> wfEnd",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestGenerateExplicitSuccessReplacesImplicitEdge(t *testing.T) {
	// step0 jumps over step1 on success: the implicit s0 --> s1 must not
	// be drawn, only the explicit s0 --> s2.
	wf := model.Workflow{
		Steps: []model.Step{
			{StepID: "a", OnSuccess: []model.SuccessAction{{Type: "goto", StepID: "c"}}},
			{StepID: "b"},
			{StepID: "c"},
		},
	}
	out := Generate(wf)

	if !strings.Contains(out, "s0 --> s2") {
		t.Errorf("missing explicit success edge s0 --> s2\n---\n%s", out)
	}
	if strings.Contains(out, "s0 --> s1") {
		t.Errorf("implicit sequential edge should be suppressed by onSuccess\n---\n%s", out)
	}
}

func TestGenerateFailurePathsAreDotted(t *testing.T) {
	wf := model.Workflow{
		Steps: []model.Step{
			{StepID: "add", OperationID: "addToCart"},
			{
				StepID:      "pay",
				OperationID: "processPayment",
				OnSuccess:   []model.SuccessAction{{Type: "end"}},
				OnFailure: []model.FailureAction{
					{Type: "retry", RetryLimit: 2, RetryLimitSet: true, RetryAfter: 2},
					{Type: "goto", StepID: "add"},
				},
			},
		},
	}
	out := Generate(wf)

	for _, want := range []string{
		"s0 --> s1",                        // implicit success on the first step
		"s1 --> wfEnd",                     // onSuccess end -> solid to End
		`s1 -. "retry x2 after 2s" .-> s1`, // retry self-loop, dotted
		`s1 -. "on failure" .-> s0`,        // onFailure goto, dotted
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestGenerateGotoOtherWorkflowDeclaresSubroutine(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "branch",
		Steps: []model.Step{
			{
				StepID:    "decide",
				OnSuccess: []model.SuccessAction{{Type: "goto", WorkflowID: "confirm-order"}},
			},
		},
	}
	out := Generate(wf)

	if !strings.Contains(out, `wf_confirm_order[["confirm-order"]]`) {
		t.Errorf("missing external workflow subroutine node\n---\n%s", out)
	}
	if !strings.Contains(out, "s0 --> wf_confirm_order") {
		t.Errorf("missing solid success edge to external workflow\n---\n%s", out)
	}
}

func TestGenerateRetryDefaultsToOne(t *testing.T) {
	wf := model.Workflow{
		Steps: []model.Step{
			{StepID: "s", OnFailure: []model.FailureAction{{Type: "retry"}}}, // RetryLimitSet false
		},
	}
	out := Generate(wf)
	if !strings.Contains(out, `"retry x1"`) {
		t.Errorf("absent retryLimit should default to x1\n---\n%s", out)
	}
}

func TestGenerateEscapesDoubleQuotesInLabel(t *testing.T) {
	wf := model.Workflow{
		Steps: []model.Step{{StepID: `a"b`}},
	}
	out := Generate(wf)
	if strings.Contains(out, `a"b`) {
		t.Errorf("raw double quote leaked into a label\n---\n%s", out)
	}
	if !strings.Contains(out, "a#34;b") {
		t.Errorf("double quote should become the #34; entity\n---\n%s", out)
	}
}

func TestGenerateEmptyWorkflowLinksStartToEnd(t *testing.T) {
	out := Generate(model.Workflow{WorkflowID: "empty"})
	if !strings.Contains(out, "wfStart --> wfEnd") {
		t.Errorf("empty workflow should link start to end\n---\n%s", out)
	}
}

func TestGenerateLabelsOperationPathAndWorkflowSteps(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "mixed",
		Steps: []model.Step{
			{StepID: "by-path", OperationPath: "{$sourceDescriptions.shop.url}#/paths/~1pet~1findByStatus/get"},
			{StepID: "by-workflow", WorkflowID: "checkout"},
			{StepID: "opaque", OperationPath: "not-the-spec-form"},
		},
	}
	out := Generate(wf)
	for _, want := range []string{
		`s0["01 by-path<br/>GET /pet/findByStatus"]`,
		`s1["02 by-workflow<br/>workflow: checkout"]`,
		// An undecodable operationPath is shown raw, like the HTML renderer.
		`s2["03 opaque<br/>not-the-spec-form"]`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestGenerateUnresolvedActionRefsKeepImplicitEdges(t *testing.T) {
	// A step whose only actions are unresolved component references must
	// not become a dead end: the implicit continue edge survives.
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "a", OperationID: "opA",
				OnSuccess: []model.SuccessAction{{Reference: "$components.successActions.ghost"}},
				OnFailure: []model.FailureAction{{Reference: "$components.failureActions.ghost"}}},
			{StepID: "b", OperationID: "opB"},
		},
	}
	out := Generate(wf)
	if !strings.Contains(out, "s0 --> s1") {
		t.Errorf("implicit success edge missing:\n%s", out)
	}
	if strings.Contains(out, "on failure") {
		t.Errorf("unresolved failure ref must not draw an edge:\n%s", out)
	}
}

func TestGenerateInheritedActionsKeepImplicitChain(t *testing.T) {
	// A workflow-level success action is merged into every step; it must
	// draw its edge without disconnecting the step chain.
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "a", OperationID: "opA",
				OnSuccess: []model.SuccessAction{{Name: "finish", Type: "end", Inherited: true}}},
			{StepID: "b", OperationID: "opB",
				OnSuccess: []model.SuccessAction{{Name: "finish", Type: "end", Inherited: true}}},
		},
	}
	out := Generate(wf)
	for _, want := range []string{"s0 --> s1", "s1 --> wfEnd", "s0 --> wfEnd"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing edge %q:\n%s", want, out)
		}
	}
}
