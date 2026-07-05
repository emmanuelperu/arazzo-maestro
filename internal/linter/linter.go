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

// Lint runs all passes. An empty raw skips the schema pass; an empty
// basePath skips cross-file validation.
func Lint(raw []byte, doc *model.ArazzoDocument, basePath string) []Issue {
	var issues []Issue
	issues = append(issues, lintSchema(raw)...)
	issues = append(issues, lintSemantic(doc)...)
	issues = append(issues, lintCrossFile(doc, basePath)...)
	return issues
}

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
	workflowIDs := make(map[string]bool, len(doc.Workflows))
	for _, wf := range doc.Workflows {
		workflowIDs[wf.WorkflowID] = true
	}
	sourceNames := make(map[string]bool, len(doc.SourceDescriptions))
	for _, s := range doc.SourceDescriptions {
		sourceNames[s.Name] = true
	}
	for i := range doc.Workflows {
		issues = append(issues, lintWorkflow(&doc.Workflows[i], workflowIDs, sourceNames, doc.Components)...)
	}
	return issues
}

// checkComponentRef reports why a Reusable Object reference stayed
// unresolved (resolution happened at parse time and cleared Reference
// on success, so a non-empty ref here is always a problem).
func checkComponentRef[T any](ref, kind string, components map[string]T, path string) []Issue {
	if ref == "" {
		return nil
	}
	name, ok := parser.ComponentRefName(ref, kind)
	if !ok {
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message:  fmt.Sprintf("malformed component reference %q: expected $components.%s.<name>", ref, kind),
		}}
	}
	if _, found := components[name]; !found {
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message:  fmt.Sprintf("reference %q: component %q does not exist in components.%s", ref, name, kind),
		}}
	}
	return nil
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

func lintWorkflow(wf *model.Workflow, workflowIDs, sourceNames map[string]bool, comps model.Components) []Issue {
	var issues []Issue
	path := "workflows[" + wf.WorkflowID + "]"

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

	// Each dependsOn entry must name a workflow of this document or use
	// the $sourceDescriptions.<name>.<workflowId> form.
	for _, dep := range wf.DependsOn {
		if dep == wf.WorkflowID {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     path + ".dependsOn",
				Message:  fmt.Sprintf("workflow %q cannot depend on itself", dep),
			})
			continue
		}
		issues = append(issues, checkWorkflowRef(dep, workflowIDs, sourceNames, path+".dependsOn")...)
	}

	// Workflow-level defaults are validated once, here; their inherited
	// per-step copies are skipped below. A workflow-level default applies
	// to every step, so its expressions may reference any declared step
	// (stepIndex -1, like workflow outputs).
	for pi, p := range wf.Parameters {
		if p.Reference != "" {
			issues = append(issues, checkComponentRef(p.Reference, "parameters", comps.Parameters, fmt.Sprintf("%s.parameters[%d]", path, pi))...)
			continue
		}
		if s, ok := p.Value.(string); ok {
			issues = append(issues, checkStepsRef(s, wf, path+".parameters."+p.Name, -1)...)
			issues = append(issues, warnDefaultStepsRef(s, path+".parameters."+p.Name)...)
		}
	}
	for ai, a := range wf.SuccessActions {
		if a.Reference != "" {
			issues = append(issues, checkComponentRef(a.Reference, "successActions", comps.SuccessActions, fmt.Sprintf("%s.successActions[%d]", path, ai))...)
			continue
		}
		issues = append(issues, checkAction(a.Type, a.StepID, a.WorkflowID, a.Criteria, wf, workflowIDs, sourceNames, path+".successActions["+a.Name+"]", -1)...)
		for _, c := range a.Criteria {
			issues = append(issues, warnDefaultStepsRef(c.Condition, path+".successActions["+a.Name+"].criteria")...)
		}
	}
	for ai, a := range wf.FailureActions {
		if a.Reference != "" {
			issues = append(issues, checkComponentRef(a.Reference, "failureActions", comps.FailureActions, fmt.Sprintf("%s.failureActions[%d]", path, ai))...)
			continue
		}
		issues = append(issues, checkAction(a.Type, a.StepID, a.WorkflowID, a.Criteria, wf, workflowIDs, sourceNames, path+".failureActions["+a.Name+"]", -1)...)
		for _, c := range a.Criteria {
			issues = append(issues, warnDefaultStepsRef(c.Condition, path+".failureActions["+a.Name+"].criteria")...)
		}
	}

	// Step-level expressions can only reference outputs of earlier steps.
	for i, step := range wf.Steps {
		stepPath := fmt.Sprintf("%s.steps[%s]", path, step.StepID)
		if step.WorkflowID != "" {
			issues = append(issues, checkWorkflowRef(step.WorkflowID, workflowIDs, sourceNames, stepPath+".workflowId")...)
		}
		for pi, p := range step.Parameters {
			if p.Inherited {
				// Validated once at the workflow level above.
				continue
			}
			if p.Reference != "" {
				// Unresolved reusable entry: its (override) value is never
				// used, so only the reference problem is worth reporting.
				issues = append(issues, checkComponentRef(p.Reference, "parameters", comps.Parameters, fmt.Sprintf("%s.parameters[%d]", stepPath, pi))...)
				continue
			}
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
		for ai, a := range step.OnSuccess {
			if a.Inherited {
				continue
			}
			if a.Reference != "" {
				// Unresolved reusable entry: the action never runs, so only
				// the reference problem is worth reporting.
				issues = append(issues, checkComponentRef(a.Reference, "successActions", comps.SuccessActions, fmt.Sprintf("%s.onSuccess[%d]", stepPath, ai))...)
				continue
			}
			issues = append(issues, checkAction(a.Type, a.StepID, a.WorkflowID, a.Criteria, wf, workflowIDs, sourceNames, stepPath+".onSuccess["+a.Name+"]", i)...)
		}
		for ai, a := range step.OnFailure {
			if a.Inherited {
				continue
			}
			if a.Reference != "" {
				issues = append(issues, checkComponentRef(a.Reference, "failureActions", comps.FailureActions, fmt.Sprintf("%s.onFailure[%d]", stepPath, ai))...)
				continue
			}
			issues = append(issues, checkAction(a.Type, a.StepID, a.WorkflowID, a.Criteria, wf, workflowIDs, sourceNames, stepPath+".onFailure["+a.Name+"]", i)...)
		}
	}
	return issues
}

// checkAction validates an action's transition target and criteria.
// Per the spec, an action stepId MUST be within the current workflow;
// a workflowId references a workflow of this document, or another
// document via the $sourceDescriptions.<name>.<workflowId> form; both
// are only relevant when the type is goto or retry.
func checkAction(typ, stepID, workflowID string, criteria []model.SuccessCriterion, wf *model.Workflow, workflowIDs, sourceNames map[string]bool, path string, stepIndex int) []Issue {
	var issues []Issue
	targetRelevant := typ == "goto" || typ == "retry"
	if stepID != "" {
		if !targetRelevant {
			issues = append(issues, Issue{
				Severity: SeverityWarning,
				Path:     path,
				Message:  fmt.Sprintf("stepId has no effect when type is %q", typ),
			})
		} else if _, step := findStep(wf, stepID); step == nil {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Path:     path,
				Message:  fmt.Sprintf("target step %q does not exist in this workflow", stepID),
			})
		}
	}
	if workflowID != "" {
		if !targetRelevant {
			issues = append(issues, Issue{
				Severity: SeverityWarning,
				Path:     path,
				Message:  fmt.Sprintf("workflowId has no effect when type is %q", typ),
			})
		} else {
			issues = append(issues, checkWorkflowRef(workflowID, workflowIDs, sourceNames, path)...)
		}
	}
	// Action criteria evaluate after the step has run, so the current
	// step's outputs are in scope: only later steps are out of reach. A
	// workflow-level action (stepIndex -1) applies to every step, so any
	// declared step may be referenced.
	critIndex := stepIndex + 1
	if stepIndex < 0 {
		critIndex = -1
	}
	for _, c := range criteria {
		issues = append(issues, checkStepsRef(c.Condition, wf, path+".criteria", critIndex)...)
	}
	return issues
}

// checkWorkflowRef validates a workflowId reference (an action target or
// a workflow-invoking step): a plain id must name a workflow of this
// document, and the $sourceDescriptions.<name>.<workflowId> form must be
// well-formed and name a declared source description (the referenced
// document itself is not loaded).
func checkWorkflowRef(workflowID string, workflowIDs, sourceNames map[string]bool, path string) []Issue {
	switch {
	case strings.HasPrefix(workflowID, "$sourceDescriptions."):
		rest := strings.TrimPrefix(workflowID, "$sourceDescriptions.")
		name, target, found := strings.Cut(rest, ".")
		if !found || name == "" || target == "" {
			return []Issue{{
				Severity: SeverityError,
				Path:     path,
				Message:  fmt.Sprintf("malformed workflowId reference %q: expected $sourceDescriptions.<name>.<workflowId>", workflowID),
			}}
		}
		if !sourceNames[name] {
			return []Issue{{
				Severity: SeverityError,
				Path:     path,
				Message:  fmt.Sprintf("workflowId reference %q: source description %q does not exist", workflowID, name),
			}}
		}
	case !workflowIDs[workflowID]:
		return []Issue{{
			Severity: SeverityError,
			Path:     path,
			Message:  fmt.Sprintf("target workflow %q does not exist in this document", workflowID),
		}}
	}
	return nil
}

// warnDefaultStepsRef warns when a workflow-level default carries a
// $steps reference: the default applies to every step, including the
// referenced step itself and the ones that run before it, where the
// output does not exist yet. Existence and output checks still run via
// checkStepsRef; this only surfaces the ordering hazard.
func warnDefaultStepsRef(expr, path string) []Issue {
	var issues []Issue
	for _, m := range stepsRefRe.FindAllStringSubmatch(expr, -1) {
		issues = append(issues, Issue{
			Severity: SeverityWarning,
			Path:     path,
			Message:  fmt.Sprintf("workflow-level default references $steps.%s.outputs.%s, which does not exist yet for steps running before %q", m[1], m[2], m[1]),
		})
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
