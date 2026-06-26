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
