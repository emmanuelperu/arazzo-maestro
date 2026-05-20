package linter

import (
	"path/filepath"
	"strings"
	"testing"

	"arazzo-maestro/internal/parser"
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
