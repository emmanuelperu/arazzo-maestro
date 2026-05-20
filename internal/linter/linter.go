// Package linter validates an Arazzo document in three passes:
//
//  1. JSON Schema (schema.go), official OAI Arazzo schema, catches the
//     structural bulk (types, required fields, enums, formats).
//  2. Internal semantic rules (this file), cross-cutting checks that
//     JSON Schema cannot express: unique IDs across collections,
//     `$steps.X.outputs.Y` reference resolution.
//  3. Cross-file resolution (crossfile.go), loads each
//     `sourceDescriptions[].url` from disk and validates that every
//     step `operationId` points at an operation that actually exists.
//
// Pass 3 is skipped if no base path is provided (tests, programmatic
// use without a file root).
package linter

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/parser"
)

// Severity classifies a linter finding.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is a single linter finding.
type Issue struct {
	Severity Severity
	Path     string
	Message  string
}

func (i Issue) String() string {
	return fmt.Sprintf("[%s] %s: %s", i.Severity, i.Path, i.Message)
}

// stepsRefRe matches references of the form `$steps.<stepId>.outputs.<name>`.
// stepId can contain hyphens; name is alphanumeric (with hyphen/underscore).
var stepsRefRe = regexp.MustCompile(`\$steps\.([A-Za-z0-9_\-]+)\.outputs\.([A-Za-z0-9_\-]+)`)

// LintFile is the high-level entry point used by the CLI. It reads the
// file, parses it, and runs the full three-pass linter.
func LintFile(path string) ([]Issue, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc, parseErr := parser.ParseBytes(raw)
	// We deliberately don't bail on parse errors: the JSON Schema pass
	// can give a more precise diagnosis on a malformed document.
	issues := Lint(raw, doc, filepath.Dir(path))
	if parseErr != nil && len(issues) == 0 {
		issues = append(issues, Issue{
			Severity: SeverityError,
			Path:     "<document>",
			Message:  parseErr.Error(),
		})
	}
	return issues, nil
}

// Lint runs all available passes. `raw` enables the JSON Schema pass;
// `basePath` enables cross-file validation. Either can be empty when
// the caller already has a fully-parsed document or doesn't need a
// particular pass.
func Lint(raw []byte, doc *model.ArazzoDocument, basePath string) []Issue {
	var issues []Issue
	issues = append(issues, lintSchema(raw)...)
	issues = append(issues, lintSemantic(doc)...)
	issues = append(issues, lintCrossFile(doc, basePath)...)
	return issues
}

// lintSemantic runs the cross-cutting semantic rules that don't fit
// into JSON Schema (uniqueness, $steps refs).
func lintSemantic(doc *model.ArazzoDocument) []Issue {
	var issues []Issue
	if doc == nil {
		return issues
	}
	if strings.TrimSpace(doc.Arazzo) == "" {
		issues = append(issues, Issue{
			Severity: SeverityError,
			Path:     "arazzo",
			Message:  "missing required top-level field 'arazzo'",
		})
	}
	if len(doc.Workflows) == 0 {
		issues = append(issues, Issue{
			Severity: SeverityWarning,
			Path:     "workflows",
			Message:  "no workflows defined",
		})
	}
	issues = append(issues, checkUniqueWorkflowIDs(doc)...)
	for i := range doc.Workflows {
		issues = append(issues, lintWorkflow(&doc.Workflows[i])...)
	}
	return issues
}

func checkUniqueWorkflowIDs(doc *model.ArazzoDocument) []Issue {
	var issues []Issue
	seen := make(map[string]int, len(doc.Workflows))
	for _, wf := range doc.Workflows {
		if wf.WorkflowID == "" {
			continue
		}
		seen[wf.WorkflowID]++
	}
	for id, n := range seen {
		if n > 1 {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     "workflows",
				Message:  fmt.Sprintf("duplicate workflowId %q (%d occurrences)", id, n),
			})
		}
	}
	return issues
}

func lintWorkflow(wf *model.Workflow) []Issue {
	var issues []Issue
	path := "workflows[" + wf.WorkflowID + "]"

	// Step IDs unique within the workflow.
	seenSteps := make(map[string]int, len(wf.Steps))
	for _, step := range wf.Steps {
		if step.StepID == "" {
			continue
		}
		seenSteps[step.StepID]++
	}
	for id, n := range seenSteps {
		if n > 1 {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     path + ".steps",
				Message:  fmt.Sprintf("duplicate stepId %q (%d occurrences)", id, n),
			})
		}
	}

	// Workflow-level outputs may reference any declared step's outputs.
	for _, out := range wf.Outputs {
		issues = append(issues, checkStepsRef(out.Expression, wf, path+".outputs."+out.Name, -1)...)
	}

	// Step-level expressions can only reference outputs of earlier steps.
	for i, step := range wf.Steps {
		stepPath := fmt.Sprintf("%s.steps[%s]", path, step.StepID)
		for _, p := range step.Parameters {
			if s, ok := p.Value.(string); ok {
				issues = append(issues, checkStepsRef(s, wf, stepPath+".parameters."+p.Name, i)...)
			}
		}
		if step.RequestBody != nil {
			issues = append(issues, checkStepsRefInValue(step.RequestBody.Payload, wf, stepPath+".requestBody.payload", i)...)
		}
		for _, out := range step.Outputs {
			issues = append(issues, checkStepsRef(out.Expression, wf, stepPath+".outputs."+out.Name, i)...)
		}
		for _, c := range step.SuccessCriteria {
			issues = append(issues, checkStepsRef(c.Condition, wf, stepPath+".successCriteria", i)...)
		}
	}
	return issues
}

// checkStepsRef inspects a single string for `$steps.X.outputs.Y` refs.
// stepIndex is the index of the step holding the expression, refs may
// only point at earlier steps (or at the same step if stepIndex == -1,
// meaning the expression is on the workflow itself).
func checkStepsRef(expr string, wf *model.Workflow, path string, stepIndex int) []Issue {
	if expr == "" {
		return nil
	}
	matches := stepsRefRe.FindAllStringSubmatch(expr, -1)
	if len(matches) == 0 {
		return nil
	}
	var issues []Issue
	for _, m := range matches {
		targetStep, targetOutput := m[1], m[2]
		idx, step := findStep(wf, targetStep)
		if step == nil {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     path,
				Message:  fmt.Sprintf("reference $steps.%s.outputs.%s: step %q does not exist", targetStep, targetOutput, targetStep),
			})
			continue
		}
		if stepIndex >= 0 && idx >= stepIndex {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     path,
				Message:  fmt.Sprintf("reference $steps.%s.outputs.%s: step %q is not declared before this one", targetStep, targetOutput, targetStep),
			})
			continue
		}
		if !hasOutput(step, targetOutput) {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     path,
				Message:  fmt.Sprintf("reference $steps.%s.outputs.%s: step %q does not declare output %q", targetStep, targetOutput, targetStep, targetOutput),
			})
		}
	}
	return issues
}

// checkStepsRefInValue walks an arbitrary YAML-decoded value and runs
// checkStepsRef on every string it contains.
func checkStepsRefInValue(v any, wf *model.Workflow, path string, stepIndex int) []Issue {
	switch t := v.(type) {
	case string:
		return checkStepsRef(t, wf, path, stepIndex)
	case map[string]any:
		var out []Issue
		for k, val := range t {
			out = append(out, checkStepsRefInValue(val, wf, path+"."+k, stepIndex)...)
		}
		return out
	case []any:
		var out []Issue
		for i, val := range t {
			out = append(out, checkStepsRefInValue(val, wf, fmt.Sprintf("%s[%d]", path, i), stepIndex)...)
		}
		return out
	}
	return nil
}

func findStep(wf *model.Workflow, stepID string) (int, *model.Step) {
	for i := range wf.Steps {
		if wf.Steps[i].StepID == stepID {
			return i, &wf.Steps[i]
		}
	}
	return -1, nil
}

func hasOutput(step *model.Step, name string) bool {
	for _, o := range step.Outputs {
		if o.Name == name {
			return true
		}
	}
	return false
}
