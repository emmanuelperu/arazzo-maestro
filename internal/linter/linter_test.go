package linter

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/emmanuelperu/arazzo-maestro/internal/parser"
)

// TestLintFileCleanExample is the end-to-end happy path: shop.arazzo.yaml
// (which references openapi.yaml in the same dir) should produce zero
// findings through all three passes.
func TestLintFileCleanExample(t *testing.T) {
	issues, err := LintFile(filepath.Join("..", "..", "examples", "shop.arazzo.yaml"))
	if err != nil {
		t.Fatalf("LintFile: %v", err)
	}
	for _, issue := range issues {
		t.Errorf("unexpected finding: %s", issue)
	}
}

// The semantic-rule tests below isolate `lintSemantic` so we don't
// have to ship full Arazzo documents that pass the JSON Schema for
// every micro-rule.

func TestSemanticDetectsDuplicateWorkflowID(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: same
    steps:
      - stepId: a
  - workflowId: same
    steps:
      - stepId: b
`
	doc, err := parser.ParseBytes([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	issues := lintSemantic(doc)
	if !containsMessage(issues, "duplicate workflowId") {
		t.Errorf("expected duplicate workflowId issue, got %v", issues)
	}
}

func TestSemanticDetectsDuplicateStepID(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: x
      - stepId: x
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, "duplicate stepId") {
		t.Errorf("expected duplicate stepId issue, got %v", issues)
	}
}

func TestSemanticDetectsUnknownStepReference(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        outputs:
          token: $response.body#/token
    outputs:
      tok: $steps.ghost.outputs.token
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, `step "ghost" does not exist`) {
		t.Errorf("expected unknown-step issue, got %v", issues)
	}
}

func TestSemanticDetectsUnknownOutputReference(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        outputs:
          token: $response.body#/token
    outputs:
      tok: $steps.a.outputs.nope
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, `does not declare output "nope"`) {
		t.Errorf("expected missing-output issue, got %v", issues)
	}
}

func TestSemanticDetectsForwardReferenceBetweenSteps(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        parameters:
          - name: from-later
            in: query
            value: $steps.b.outputs.token
      - stepId: b
        outputs:
          token: $response.body#/token
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, "is not declared before") {
		t.Errorf("expected forward-reference issue, got %v", issues)
	}
}

func TestSemanticDetectsDanglingActionStepTarget(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: pay
        onSuccess:
          - name: go-confirm
            type: goto
            stepId: ghost
        onFailure:
          - name: retry-other
            type: retry
            stepId: phantom
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, `target step "ghost" does not exist`) {
		t.Errorf("expected dangling onSuccess goto issue, got %v", issues)
	}
	if !containsMessage(issues, `target step "phantom" does not exist`) {
		t.Errorf("expected dangling onFailure retry issue, got %v", issues)
	}
}

func TestSemanticDetectsDanglingActionWorkflowTarget(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: pay
        onFailure:
          - name: go-cleanup
            type: goto
            workflowId: missing-workflow
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, `target workflow "missing-workflow" does not exist`) {
		t.Errorf("expected dangling workflow target issue, got %v", issues)
	}
}

func TestSemanticValidatesSourceDescriptionWorkflowTarget(t *testing.T) {
	src := `
arazzo: "1.1.0"
sourceDescriptions:
  - name: other
    url: ./other.arazzo.yaml
    type: arazzo
workflows:
  - workflowId: wf
    steps:
      - stepId: pay
        onSuccess:
          - name: known-source
            type: goto
            workflowId: $sourceDescriptions.other.cleanup
          - name: unknown-source
            type: goto
            workflowId: $sourceDescriptions.ghost.cleanup
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, `source description "ghost" does not exist`) {
		t.Errorf("expected unknown source-description issue, got %v", issues)
	}
	if containsMessage(issues, `source description "other"`) {
		t.Errorf("known source description must not be flagged, got %v", issues)
	}
}

func TestSemanticDetectsForwardReferenceInActionCriteria(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        onFailure:
          - name: retry-on-flag
            type: retry
            criteria:
              - condition: $steps.b.outputs.flag == "yes"
      - stepId: b
        outputs:
          flag: $response.body#/flag
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, "is not declared before") {
		t.Errorf("expected forward-reference issue in action criteria, got %v", issues)
	}
}

func TestSemanticAllowsCurrentStepRefInActionCriteria(t *testing.T) {
	// Action criteria evaluate after the step has run, so referencing
	// the step's own outputs is legal; only later steps are out of
	// reach.
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: pay
        outputs:
          status: $response.body#/status
        onFailure:
          - name: retry-pending
            type: retry
            criteria:
              - condition: $steps.pay.outputs.status == "pending"
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	for _, i := range issues {
		t.Errorf("unexpected issue: %s", i)
	}
}

func TestSemanticDetectsMalformedSourceDescriptionWorkflowRef(t *testing.T) {
	src := `
arazzo: "1.1.0"
sourceDescriptions:
  - name: other
    url: ./other.arazzo.yaml
    type: arazzo
workflows:
  - workflowId: wf
    steps:
      - stepId: pay
        onSuccess:
          - name: no-target
            type: goto
            workflowId: $sourceDescriptions.other
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, "malformed workflowId reference") {
		t.Errorf("expected malformed-reference issue, got %v", issues)
	}
}

func TestSemanticWarnsOnIrrelevantActionTarget(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: pay
        onSuccess:
          - name: stop
            type: end
            stepId: pay
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, `stepId has no effect when type is "end"`) {
		t.Errorf("expected no-effect warning, got %v", issues)
	}
}

func TestSemanticAcceptsValidActionTargets(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        outputs:
          flag: $response.body#/flag
      - stepId: pay
        onSuccess:
          - name: go-back
            type: goto
            stepId: a
        onFailure:
          - name: retry-on-flag
            type: retry
            criteria:
              - condition: $steps.a.outputs.flag == "yes"
  - workflowId: cleanup
    steps:
      - stepId: c
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	for _, i := range issues {
		t.Errorf("unexpected issue: %s", i)
	}
}

func TestIssueString(t *testing.T) {
	i := Issue{Severity: SeverityError, Path: "workflows[a]", Message: "bad"}
	s := i.String()
	for _, want := range []string{"error", "workflows[a]", "bad"} {
		if !strings.Contains(s, want) {
			t.Errorf("Issue.String() missing %q in %q", want, s)
		}
	}
}

func containsMessage(issues []Issue, substr string) bool {
	for _, i := range issues {
		if strings.Contains(i.Message, substr) {
			return true
		}
	}
	return false
}

func TestSemanticValidatesStepWorkflowID(t *testing.T) {
	src := `
arazzo: "1.1.0"
sourceDescriptions:
  - name: other
    url: ./other.arazzo.yaml
    type: arazzo
workflows:
  - workflowId: wf
    steps:
      - stepId: local-ok
        workflowId: nested
      - stepId: local-missing
        workflowId: ghost-flow
      - stepId: qualified-ok
        workflowId: $sourceDescriptions.other.cleanup
      - stepId: qualified-missing
        workflowId: $sourceDescriptions.ghost.cleanup
      - stepId: malformed
        workflowId: $sourceDescriptions.other
  - workflowId: nested
    steps:
      - stepId: noop
        operationId: ping
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, `target workflow "ghost-flow" does not exist`) {
		t.Errorf("expected dangling step workflowId issue, got %v", issues)
	}
	if !containsMessage(issues, `source description "ghost" does not exist`) {
		t.Errorf("expected unknown source-description issue, got %v", issues)
	}
	if !containsMessage(issues, `malformed workflowId reference "$sourceDescriptions.other"`) {
		t.Errorf("expected malformed reference issue, got %v", issues)
	}
	for _, i := range issues {
		if strings.Contains(i.Path, "steps[local-ok]") || strings.Contains(i.Path, "steps[qualified-ok]") {
			t.Errorf("valid step workflowId flagged: %s", i)
		}
	}
}

func TestSemanticValidatesComponentReferences(t *testing.T) {
	src := `
arazzo: "1.1.0"
components:
  parameters:
    page-size: { name: pageSize, in: query, value: 20 }
  successActions:
    finish: { name: finish, type: end }
  failureActions:
    give-up: { name: give-up, type: end }
workflows:
  - workflowId: wf
    steps:
      - stepId: list
        operationId: listThings
        parameters:
          - reference: $components.parameters.page-size
          - reference: $components.parameters.ghost
          - reference: $inputs.pageSize
        onSuccess:
          - reference: $components.successActions.finish
          - reference: $components.failureActions.give-up
        onFailure:
          - reference: $components.failureActions.give-up
          - reference: $components.failureActions.missing
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, `component "ghost" does not exist in components.parameters`) {
		t.Errorf("expected dangling parameter component issue, got %v", issues)
	}
	if !containsMessage(issues, `malformed component reference "$inputs.pageSize"`) {
		t.Errorf("expected malformed reference issue, got %v", issues)
	}
	// A successActions slot referencing a failureActions component is a
	// kind mismatch, reported as malformed for that slot.
	if !containsMessage(issues, `malformed component reference "$components.failureActions.give-up": expected $components.successActions.<name>`) {
		t.Errorf("expected kind-mismatch issue, got %v", issues)
	}
	if !containsMessage(issues, `component "missing" does not exist in components.failureActions`) {
		t.Errorf("expected dangling failure action issue, got %v", issues)
	}
	for _, i := range issues {
		if strings.Contains(i.Message, "page-size") || strings.Contains(i.Message, `"finish"`) {
			t.Errorf("valid component reference flagged: %s", i)
		}
	}
}

func TestSemanticValidatesDependsOn(t *testing.T) {
	src := `
arazzo: "1.1.0"
sourceDescriptions:
  - name: other
    url: ./other.arazzo.yaml
    type: arazzo
workflows:
  - workflowId: wf
    dependsOn:
      - warmup
      - ghost-flow
      - $sourceDescriptions.other.prepare
      - $sourceDescriptions.ghost.prepare
    steps:
      - stepId: a
        operationId: op
  - workflowId: warmup
    steps:
      - stepId: noop
        operationId: op
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, `target workflow "ghost-flow" does not exist`) {
		t.Errorf("expected dangling dependsOn issue, got %v", issues)
	}
	if !containsMessage(issues, `source description "ghost" does not exist`) {
		t.Errorf("expected unknown source in dependsOn, got %v", issues)
	}
	for _, i := range issues {
		if strings.Contains(i.Message, `"warmup"`) || strings.Contains(i.Message, `"other"`) {
			t.Errorf("valid dependsOn entry flagged: %s", i)
		}
	}
}

func TestSemanticValidatesWorkflowLevelDefaultsOnce(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    parameters:
      - reference: $components.parameters.ghost
    successActions:
      - name: jump
        type: goto
        stepId: nowhere
    failureActions:
      - name: bail
        type: end
        criteria:
          - condition: $steps.b.outputs.token == "x"
    steps:
      - stepId: a
        operationId: op
      - stepId: b
        operationId: op2
        outputs:
          token: $response.body#/token
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	dangling, target := 0, 0
	for _, i := range issues {
		if strings.Contains(i.Message, `component "ghost" does not exist`) {
			dangling++
			if !strings.Contains(i.Path, "workflows[wf].parameters[0]") {
				t.Errorf("workflow-level ref reported at wrong path: %s", i)
			}
		}
		if strings.Contains(i.Message, `target step "nowhere" does not exist`) {
			target++
			if !strings.Contains(i.Path, "workflows[wf].successActions[jump]") {
				t.Errorf("workflow-level action reported at wrong path: %s", i)
			}
		}
		// A workflow-level criteria may reference any declared step
		// (no existence ERROR), but the ordering hazard is warned.
		if i.Severity == SeverityError && strings.Contains(i.Message, "$steps.b.outputs.token") {
			t.Errorf("workflow-level criteria ref wrongly errored: %s", i)
		}
	}
	// Exactly once each, not once per inheriting step.
	if dangling != 1 || target != 1 {
		t.Errorf("workflow-level issues must be reported once (got dangling=%d target=%d): %v", dangling, target, issues)
	}
	if !containsMessage(issues, "does not exist yet for steps running before") {
		t.Errorf("expected the workflow-level ordering warning, got %v", issues)
	}
}

func TestSemanticRejectsSelfDependency(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    dependsOn: [wf]
    steps:
      - stepId: a
        operationId: op
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, `workflow "wf" cannot depend on itself`) {
		t.Errorf("expected self-dependency issue, got %v", issues)
	}
}

func TestSemanticChecksCriterionContextRefs(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        operationId: op
        successCriteria:
          - context: $steps.ghost.outputs.body
            condition: $.id == 1
            type: jsonpath
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintSemantic(doc)
	if !containsMessage(issues, `step "ghost" does not exist`) {
		t.Errorf("expected context reference issue, got %v", issues)
	}
}
