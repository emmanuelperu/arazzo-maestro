// Package mermaidgen renders an Arazzo workflow as a Mermaid flowchart.
//
// Mermaid is a text DSL rendered client-side (GitHub, mermaid.js, IDEs),
// so generation is pure string building with no dependency and no
// rendering step, which keeps it aligned with the project's
// offline-first and eco-design rules.
//
// Mapping (top-down flowchart). Success paths are solid edges, failure
// paths are dotted, so the two read apart at a glance:
//
//	workflow start             -> a [Start] stadium node
//	each step                  -> a box "NN stepId / operationId"
//	step with no onSuccess      -> solid edge to the next step (last -> [End])
//	onSuccess goto              -> solid edge to the target step
//	onSuccess goto <workflow>   -> solid edge to a subroutine node
//	onSuccess end               -> solid edge to [End]
//	onFailure goto / end        -> dotted "on failure" edge
//	onFailure retry             -> dotted self-loop "retry xN"
//
// An explicit onSuccess action replaces the implicit "continue to the
// next step", matching the Arazzo control-flow semantics. Step criteria
// are not put on edge labels, the labels stay fixed safe strings.
package mermaidgen

import (
	"fmt"
	"strings"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
)

// Reserved node ids for the workflow terminals. Step nodes are "s0",
// "s1", ... and external-workflow nodes are "wf_<sanitized id>", neither
// of which can collide with these.
const (
	startNode = "wfStart"
	endNode   = "wfEnd"
)

// Generate returns the Mermaid flowchart source for a single workflow.
// The output is raw Mermaid text (no ``` fence) so it can be written to a
// .mmd file or wrapped in a fenced block by the caller.
func Generate(wf model.Workflow) string {
	f := &flowchart{
		nodeOf: make(map[string]string, len(wf.Steps)),
		ext:    make(map[string]struct{}),
	}
	for i, step := range wf.Steps {
		f.nodeOf[step.StepID] = fmt.Sprintf("s%d", i)
	}

	f.line("flowchart TD")
	f.declareNodes(wf.Steps)
	f.edges(wf.Steps)
	return f.b.String()
}

// flowchart accumulates the diagram source and the lookup state shared
// across the build steps.
type flowchart struct {
	b      strings.Builder
	nodeOf map[string]string   // stepId -> node id
	ext    map[string]struct{} // already-declared external-workflow node ids
}

func (f *flowchart) line(s string) {
	f.b.WriteString(s + "\n")
}

func (f *flowchart) declareNodes(steps []model.Step) {
	f.line("  " + startNode + "([Start])")
	for i, step := range steps {
		label := fmt.Sprintf("%02d %s", i+1, step.StepID)
		if step.OperationID != "" {
			label += "<br/>" + step.OperationID
		}
		f.line(fmt.Sprintf("  s%d[%q]", i, escape(label)))
	}
	f.line("  " + endNode + "([End])")
}

// edges wires the entry edge, each step's success path (solid) and
// failure path (dotted).
func (f *flowchart) edges(steps []model.Step) {
	if len(steps) == 0 {
		f.solid(startNode, endNode)
		return
	}
	f.solid(startNode, "s0")

	for i, step := range steps {
		src := fmt.Sprintf("s%d", i)

		if len(step.OnSuccess) == 0 {
			f.solid(src, f.next(i, len(steps))) // implicit "continue"
		} else {
			for _, a := range step.OnSuccess {
				f.solid(src, f.target(a.Type, a.StepID, a.WorkflowID))
			}
		}

		for _, a := range step.OnFailure {
			if a.Type == "retry" {
				f.dotted(src, retryLabel(a), f.retryTarget(src, step.StepID, a.StepID))
				continue
			}
			f.dotted(src, "on failure", f.target(a.Type, a.StepID, a.WorkflowID))
		}
	}
}

// next is the node a step continues to when its success path is implicit:
// the following step, or End for the last one.
func (f *flowchart) next(i, total int) string {
	if i == total-1 {
		return endNode
	}
	return fmt.Sprintf("s%d", i+1)
}

// target resolves a goto/end action to its destination node id, or ""
// when the action has no drawable target (the edge is then skipped).
func (f *flowchart) target(typ, stepID, workflowID string) string {
	switch {
	case typ == "end":
		return endNode
	case workflowID != "":
		return f.declareExternal(workflowID)
	case stepID != "":
		return f.nodeOf[stepID] // "" when the id is unknown
	default:
		return ""
	}
}

// retryTarget resolves a retry action. An omitted or self-referential
// stepId (per the Arazzo spec) retries the current step.
func (f *flowchart) retryTarget(currentNode, currentStepID, targetStepID string) string {
	if targetStepID == "" || targetStepID == currentStepID {
		return currentNode
	}
	if n, ok := f.nodeOf[targetStepID]; ok {
		return n
	}
	return currentNode
}

// declareExternal emits a subroutine node for a goto to another workflow
// the first time it is seen and returns its node id.
func (f *flowchart) declareExternal(workflowID string) string {
	id := "wf_" + sanitizeID(workflowID)
	if _, done := f.ext[id]; !done {
		f.line(fmt.Sprintf("  %s[[%q]]", id, escape(workflowID)))
		f.ext[id] = struct{}{}
	}
	return id
}

func (f *flowchart) solid(src, dst string) {
	if dst == "" {
		return
	}
	f.line(fmt.Sprintf("  %s --> %s", src, dst))
}

func (f *flowchart) dotted(src, label, dst string) {
	if dst == "" {
		return
	}
	f.line(fmt.Sprintf("  %s -. %q .-> %s", src, escape(label), dst))
}

func retryLabel(a model.FailureAction) string {
	limit := 1 // the spec default when retryLimit is absent
	if a.RetryLimitSet {
		limit = a.RetryLimit
	}
	label := fmt.Sprintf("retry x%d", limit)
	if a.RetryAfter > 0 {
		label += fmt.Sprintf(" after %gs", a.RetryAfter)
	}
	return label
}

// escape neutralises the one character that would break a double-quoted
// Mermaid label, using the numeric entity code Mermaid decodes, and
// flattens newlines. The intentional <br/> line break is left intact.
func escape(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, `"`, "#34;")
	return s
}

// sanitizeID turns an arbitrary workflow id into a Mermaid-safe node id
// suffix ([A-Za-z0-9_] only).
func sanitizeID(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
