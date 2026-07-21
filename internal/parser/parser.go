// Package parser turns an Arazzo YAML document into typed model structs.
//
// The parser walks a yaml.Node tree rather than relying on struct tags so
// that:
//   - ordered keys (e.g. workflow outputs) survive into the rendered HTML;
//   - missing required fields produce a clear ArazzoParseError instead of
//     a silent zero value.
package parser

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
)

// ArazzoParseError is returned when the YAML cannot be turned into a
// model.ArazzoDocument.
type ArazzoParseError struct {
	Msg string
}

func (e *ArazzoParseError) Error() string { return e.Msg }

func errf(format string, args ...any) *ArazzoParseError {
	return &ArazzoParseError{Msg: fmt.Sprintf(format, args...)}
}

// ParseFile loads an Arazzo YAML file from disk and parses it.
func ParseFile(path string) (*model.ArazzoDocument, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseBytes(raw)
}

// ParseBytes parses an Arazzo YAML document from bytes.
func ParseBytes(raw []byte) (*model.ArazzoDocument, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, errf("invalid YAML: %s", err)
	}
	top := unwrapDocument(&root)
	if top == nil || top.Kind != yaml.MappingNode {
		return nil, errf("Top-level YAML must be a mapping")
	}
	return parseDocument(top)
}

func unwrapDocument(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		return n.Content[0]
	}
	return n
}

func parseDocument(n *yaml.Node) (*model.ArazzoDocument, error) {
	doc := &model.ArazzoDocument{}
	for _, kv := range mappingPairs(n) {
		switch kv.Key {
		case "arazzo":
			doc.Arazzo = scalarString(kv.Value)
		case "info":
			if kv.Value.Kind == yaml.MappingNode {
				for _, infoKV := range mappingPairs(kv.Value) {
					switch infoKV.Key {
					case "title":
						doc.Title = scalarString(infoKV.Value)
					case "summary":
						doc.Summary = scalarString(infoKV.Value)
					case "description":
						doc.Description = scalarString(infoKV.Value)
					case "version":
						doc.Version = scalarString(infoKV.Value)
					}
				}
			}
		case "sourceDescriptions":
			sources, err := parseSourceDescriptions(kv.Value)
			if err != nil {
				return nil, err
			}
			doc.SourceDescriptions = sources
		case "workflows":
			workflows, err := parseWorkflows(kv.Value)
			if err != nil {
				return nil, err
			}
			doc.Workflows = workflows
		case "components":
			doc.Components = parseComponents(kv.Value)
		}
	}
	resolveComponentRefs(doc)
	mergeWorkflowDefaults(doc)
	return doc, nil
}

// parseStringSequence reads a YAML sequence of scalars.
func parseStringSequence(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(n.Content))
	for _, item := range n.Content {
		out = append(out, scalarString(item))
	}
	return out
}

// mergeWorkflowDefaults copies the workflow-level parameters and
// success/failure actions into every step, per the spec: they apply to
// all steps, can be overridden at the step level (same name, plus `in`
// for parameters) and cannot be removed. Inherited copies are flagged
// so the linter validates them once at the workflow level and the
// renderer can badge their origin. Unresolved reusable entries are not
// propagated: they carry nothing and are reported at their declaration.
// The copies share inner slice/map backing (criteria, composite values)
// with the declared defaults; consumers treat the model as read-only.
func mergeWorkflowDefaults(doc *model.ArazzoDocument) {
	for wi := range doc.Workflows {
		wf := &doc.Workflows[wi]
		if len(wf.Parameters) == 0 && len(wf.SuccessActions) == 0 && len(wf.FailureActions) == 0 {
			continue
		}
		for si := range wf.Steps {
			step := &wf.Steps[si]
			step.Parameters = mergeParameters(wf.Parameters, step.Parameters)
			step.OnSuccess = mergeSuccessActions(wf.SuccessActions, step.OnSuccess)
			step.OnFailure = mergeFailureActions(wf.FailureActions, step.OnFailure)
		}
	}
}

func mergeParameters(defaults, own []model.Parameter) []model.Parameter {
	if len(defaults) == 0 {
		return own
	}
	merged := make([]model.Parameter, 0, len(defaults)+len(own))
	for _, d := range defaults {
		if d.Reference != "" || overridesParameter(own, d) {
			continue
		}
		d.Inherited = true
		merged = append(merged, d)
	}
	return append(merged, own...)
}

// overridesParameter reports whether the step declares its own
// parameter with the same identity (name + in) as the default.
func overridesParameter(own []model.Parameter, d model.Parameter) bool {
	for _, p := range own {
		if p.Reference == "" && p.Name == d.Name && p.In == d.In {
			return true
		}
	}
	return false
}

func mergeSuccessActions(defaults, own []model.SuccessAction) []model.SuccessAction {
	if len(defaults) == 0 {
		return own
	}
	names := make([]string, 0, len(own))
	for _, a := range own {
		if a.Reference == "" {
			names = append(names, a.Name)
		}
	}
	merged := make([]model.SuccessAction, 0, len(defaults)+len(own))
	for _, d := range defaults {
		if d.Reference != "" || overridesAction(names, d.Name) {
			continue
		}
		d.Inherited = true
		merged = append(merged, d)
	}
	return append(merged, own...)
}

func mergeFailureActions(defaults, own []model.FailureAction) []model.FailureAction {
	if len(defaults) == 0 {
		return own
	}
	names := make([]string, 0, len(own))
	for _, a := range own {
		if a.Reference == "" {
			names = append(names, a.Name)
		}
	}
	merged := make([]model.FailureAction, 0, len(defaults)+len(own))
	for _, d := range defaults {
		if d.Reference != "" || overridesAction(names, d.Name) {
			continue
		}
		d.Inherited = true
		merged = append(merged, d)
	}
	return append(merged, own...)
}

// overridesAction reports whether the step declares its own action with
// the same name as the default (actions are identified by name).
func overridesAction(ownNames []string, name string) bool {
	for _, n := range ownNames {
		if n == name {
			return true
		}
	}
	return false
}

// parseComponents reads the document's Components Object. Reusable
// `inputs` schemas are left to the schema pass (see model.Components).
func parseComponents(n *yaml.Node) model.Components {
	c := model.Components{}
	if n == nil || n.Kind != yaml.MappingNode {
		return c
	}
	for _, kv := range mappingPairs(n) {
		switch kv.Key {
		case "parameters":
			c.Parameters = parseComponentMap(kv.Value, parseParameter)
		case "successActions":
			c.SuccessActions = parseComponentMap(kv.Value, parseSuccessAction)
		case "failureActions":
			c.FailureActions = parseComponentMap(kv.Value, parseFailureAction)
		}
	}
	return c
}

// parseComponentMap reads one components sub-map (name to object) with
// the per-kind item parser. Non-mapping definitions are skipped (the
// schema pass reports them), so a reference to one stays unresolved
// instead of inlining an empty struct.
func parseComponentMap[T any](n *yaml.Node, parse func(*yaml.Node) T) map[string]T {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make(map[string]T, len(n.Content)/2)
	for _, kv := range mappingPairs(n) {
		if kv.Value == nil || kv.Value.Kind != yaml.MappingNode {
			continue
		}
		out[kv.Key] = parse(kv.Value)
	}
	return out
}

// resolveComponentRefs inlines every Reusable Object reference into the
// referenced component, so downstream consumers (renderer, generators)
// never deal with indirection. Inlining clears Reference, so a non-empty Reference
// after this pass means exactly "this entry did not resolve" (the
// linter reports why). Inlined structs share their slice/map backing
// with the component map; consumers treat the model as read-only.
func resolveComponentRefs(doc *model.ArazzoDocument) {
	for wi := range doc.Workflows {
		wf := &doc.Workflows[wi]
		resolveParameterRefs(wf.Parameters, doc.Components)
		resolveSuccessActionRefs(wf.SuccessActions, doc.Components)
		resolveFailureActionRefs(wf.FailureActions, doc.Components)
		for si := range wf.Steps {
			step := &wf.Steps[si]
			resolveParameterRefs(step.Parameters, doc.Components)
			resolveSuccessActionRefs(step.OnSuccess, doc.Components)
			resolveFailureActionRefs(step.OnFailure, doc.Components)
		}
	}
}

func resolveParameterRefs(params []model.Parameter, components model.Components) {
	for pi := range params {
		p := &params[pi]
		if comp, ok := resolveComponent(p.Reference, "parameters", components.Parameters); ok {
			// Per the spec (Reusable Object, `value`), a value carried
			// by the reusable entry overrides the component's value.
			// An explicit `value: null` is indistinguishable from an
			// omitted value here and keeps the component's value.
			override := p.Value
			*p = comp
			if override != nil {
				p.Value = override
			}
		}
	}
}

func resolveSuccessActionRefs(actions []model.SuccessAction, components model.Components) {
	for ai := range actions {
		a := &actions[ai]
		if comp, ok := resolveComponent(a.Reference, "successActions", components.SuccessActions); ok {
			*a = comp
		}
	}
}

func resolveFailureActionRefs(actions []model.FailureAction, components model.Components) {
	for ai := range actions {
		a := &actions[ai]
		if comp, ok := resolveComponent(a.Reference, "failureActions", components.FailureActions); ok {
			*a = comp
		}
	}
}

// resolveComponent looks a Reusable Object reference up in the
// components map of its kind. ok is false when ref is empty, malformed,
// of another kind, or names no declared component.
func resolveComponent[T any](ref, kind string, components map[string]T) (T, bool) {
	name, ok := ComponentRefName(ref, kind)
	if !ok {
		var zero T
		return zero, false
	}
	comp, found := components[name]
	return comp, found
}

// ComponentRefName extracts the component name from a Reusable Object
// reference of the given kind: `$components.<kind>.<name>`.
func ComponentRefName(ref, kind string) (string, bool) {
	prefix := "$components." + kind + "."
	if !strings.HasPrefix(ref, prefix) || len(ref) == len(prefix) {
		return "", false
	}
	return strings.TrimPrefix(ref, prefix), true
}

func parseSourceDescriptions(n *yaml.Node) ([]model.SourceDescription, error) {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil, nil
	}
	out := make([]model.SourceDescription, 0, len(n.Content))
	for _, item := range n.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		src := model.SourceDescription{}
		for _, kv := range mappingPairs(item) {
			switch kv.Key {
			case "name":
				src.Name = scalarString(kv.Value)
			case "url":
				src.URL = scalarString(kv.Value)
			case "type":
				src.Type = scalarString(kv.Value)
			}
		}
		out = append(out, src)
	}
	return out, nil
}

func parseWorkflows(n *yaml.Node) ([]model.Workflow, error) {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil, nil
	}
	out := make([]model.Workflow, 0, len(n.Content))
	for _, item := range n.Content {
		wf, err := parseWorkflow(item)
		if err != nil {
			return nil, err
		}
		out = append(out, wf)
	}
	return out, nil
}

func parseWorkflow(n *yaml.Node) (model.Workflow, error) {
	wf := model.Workflow{}
	if n == nil || n.Kind != yaml.MappingNode {
		return wf, errf("Workflow entry must be a mapping")
	}
	seenWorkflowID := false
	for _, kv := range mappingPairs(n) {
		switch kv.Key {
		case "workflowId":
			seenWorkflowID = true
			wf.WorkflowID = scalarString(kv.Value)
		case "summary":
			wf.Summary = scalarString(kv.Value)
		case "description":
			wf.Description = scalarString(kv.Value)
		case "dependsOn":
			wf.DependsOn = parseStringSequence(kv.Value)
		case "inputs":
			wf.Inputs = parseInputs(kv.Value)
		case "parameters":
			wf.Parameters = parseParameters(kv.Value)
		case "successActions":
			wf.SuccessActions = parseSuccessActions(kv.Value)
		case "failureActions":
			wf.FailureActions = parseFailureActions(kv.Value)
		case "steps":
			steps, err := parseSteps(kv.Value)
			if err != nil {
				return wf, err
			}
			wf.Steps = steps
		case "outputs":
			wf.Outputs = parseOutputs(kv.Value)
		}
	}
	if !seenWorkflowID {
		return wf, errf("Workflow is missing required field 'workflowId'")
	}
	return wf, nil
}

func parseInputs(n *yaml.Node) []model.InputProperty {
	var out []model.InputProperty
	collectInputProperties(n, "", &out)
	return out
}

// collectInputProperties walks one level of a JSON Schema object node:
// its `properties` become InputProperty entries (marked by `required`),
// and nested object properties recurse with a dotted prefix so they can
// be declared and documented individually.
func collectInputProperties(schema *yaml.Node, prefix string, out *[]model.InputProperty) {
	if schema == nil || schema.Kind != yaml.MappingNode {
		return
	}
	var properties *yaml.Node
	required := make(map[string]bool)
	for _, kv := range mappingPairs(schema) {
		switch kv.Key {
		case "properties":
			properties = kv.Value
		case "required":
			for _, name := range parseStringSequence(kv.Value) {
				required[name] = true
			}
		}
	}
	if properties == nil || properties.Kind != yaml.MappingNode {
		return
	}
	for _, kv := range mappingPairs(properties) {
		prop := model.InputProperty{Name: prefix + kv.Key, Required: required[kv.Key]}
		var nested *yaml.Node
		if kv.Value.Kind == yaml.MappingNode {
			for _, specKV := range mappingPairs(kv.Value) {
				switch specKV.Key {
				case "type":
					prop.Type = scalarString(specKV.Value)
				case "default":
					prop.Default = nodeToAny(specKV.Value)
				case "properties":
					nested = kv.Value
				}
			}
		}
		// The parent row is always kept: the whole object stays
		// referenceable ($inputs.user) and carries its own required
		// marker and default; nested properties add dotted rows.
		*out = append(*out, prop)
		if nested != nil {
			collectInputProperties(nested, prop.Name+".", out)
		}
	}
}

func parseSteps(n *yaml.Node) ([]model.Step, error) {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil, nil
	}
	out := make([]model.Step, 0, len(n.Content))
	for _, item := range n.Content {
		step, err := parseStep(item)
		if err != nil {
			return nil, err
		}
		out = append(out, step)
	}
	return out, nil
}

func parseStep(n *yaml.Node) (model.Step, error) {
	step := model.Step{}
	if n == nil || n.Kind != yaml.MappingNode {
		return step, errf("Step entry must be a mapping")
	}
	seenStepID := false
	for _, kv := range mappingPairs(n) {
		switch kv.Key {
		case "stepId":
			seenStepID = true
			step.StepID = scalarString(kv.Value)
		case "description":
			step.Description = scalarString(kv.Value)
		case "operationId":
			step.OperationID = scalarString(kv.Value)
		case "operationPath":
			step.OperationPath = scalarString(kv.Value)
		case "workflowId":
			step.WorkflowID = scalarString(kv.Value)
		case "parameters":
			step.Parameters = parseParameters(kv.Value)
		case "requestBody":
			step.RequestBody = parseRequestBody(kv.Value)
		case "successCriteria":
			step.SuccessCriteria = parseSuccessCriteria(kv.Value)
		case "outputs":
			step.Outputs = parseOutputs(kv.Value)
		case "onSuccess":
			step.OnSuccess = parseSuccessActions(kv.Value)
		case "onFailure":
			step.OnFailure = parseFailureActions(kv.Value)
		}
	}
	if !seenStepID {
		return step, errf("Step is missing required field 'stepId'")
	}
	return step, nil
}

func parseSuccessActions(n *yaml.Node) []model.SuccessAction {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]model.SuccessAction, 0, len(n.Content))
	for _, item := range n.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		out = append(out, parseSuccessAction(item))
	}
	return out
}

// parseSuccessAction reads one Success Action Object, or a Reusable
// Object entry (only `reference` is then set; resolveComponentRefs
// inlines it afterwards).
func parseSuccessAction(item *yaml.Node) model.SuccessAction {
	a := model.SuccessAction{}
	if item == nil || item.Kind != yaml.MappingNode {
		return a
	}
	for _, kv := range mappingPairs(item) {
		switch kv.Key {
		case "name":
			a.Name = scalarString(kv.Value)
		case "type":
			a.Type = scalarString(kv.Value)
		case "stepId":
			a.StepID = scalarString(kv.Value)
		case "workflowId":
			a.WorkflowID = scalarString(kv.Value)
		case "criteria":
			a.Criteria = parseSuccessCriteria(kv.Value)
		case "reference":
			a.Reference = scalarString(kv.Value)
		}
	}
	return a
}

func parseFailureActions(n *yaml.Node) []model.FailureAction {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]model.FailureAction, 0, len(n.Content))
	for _, item := range n.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		out = append(out, parseFailureAction(item))
	}
	return out
}

// parseFailureAction reads one Failure Action Object, or a Reusable
// Object entry (only `reference` is then set; resolveComponentRefs
// inlines it afterwards).
func parseFailureAction(item *yaml.Node) model.FailureAction {
	a := model.FailureAction{}
	if item == nil || item.Kind != yaml.MappingNode {
		return a
	}
	for _, kv := range mappingPairs(item) {
		switch kv.Key {
		case "name":
			a.Name = scalarString(kv.Value)
		case "type":
			a.Type = scalarString(kv.Value)
		case "stepId":
			a.StepID = scalarString(kv.Value)
		case "workflowId":
			a.WorkflowID = scalarString(kv.Value)
		case "retryAfter":
			a.RetryAfter = scalarFloat(kv.Value)
		case "retryLimit":
			a.RetryLimit = scalarInt(kv.Value)
			a.RetryLimitSet = true
		case "criteria":
			a.Criteria = parseSuccessCriteria(kv.Value)
		case "reference":
			a.Reference = scalarString(kv.Value)
		}
	}
	return a
}

// scalarFloat returns the numeric value of a scalar node, or 0 if the
// node is missing or not parseable as a number.
func scalarFloat(n *yaml.Node) float64 {
	s := scalarString(n)
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// scalarInt returns the integer value of a scalar node, or 0 if the
// node is missing or not parseable as an int.
func scalarInt(n *yaml.Node) int {
	s := scalarString(n)
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

func parseParameters(n *yaml.Node) []model.Parameter {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]model.Parameter, 0, len(n.Content))
	for _, item := range n.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		out = append(out, parseParameter(item))
	}
	return out
}

// parseParameter reads one Parameter Object, or a Reusable Object entry
// (`reference` plus an optional overriding `value`;
// resolveComponentRefs inlines it afterwards).
func parseParameter(item *yaml.Node) model.Parameter {
	param := model.Parameter{}
	if item == nil || item.Kind != yaml.MappingNode {
		return param
	}
	for _, kv := range mappingPairs(item) {
		switch kv.Key {
		case "name":
			param.Name = scalarString(kv.Value)
		case "in":
			param.In = scalarString(kv.Value)
		case "value":
			param.Value = nodeToAny(kv.Value)
		case "reference":
			param.Reference = scalarString(kv.Value)
		}
	}
	return param
}

func parseRequestBody(n *yaml.Node) *model.RequestBody {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	body := &model.RequestBody{}
	for _, kv := range mappingPairs(n) {
		switch kv.Key {
		case "contentType":
			body.ContentType = scalarString(kv.Value)
		case "payload":
			body.Payload = nodeToAny(kv.Value)
		case "replacements":
			body.Replacements = parseReplacements(kv.Value)
		}
	}
	return body
}

// parseReplacements reads the `replacements` array of Payload Replacement
// Objects. Entries missing the required `target` key are skipped (the
// schema pass reports them upstream); an explicit `target: ""` is kept,
// it is the valid RFC 6901 whole-document pointer.
func parseReplacements(n *yaml.Node) []model.Replacement {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]model.Replacement, 0, len(n.Content))
	for _, item := range n.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		var r model.Replacement
		hasTarget := false
		for _, kv := range mappingPairs(item) {
			switch kv.Key {
			case "target":
				r.Target = scalarString(kv.Value)
				hasTarget = true
			case "value":
				r.Value = nodeToAny(kv.Value)
			}
		}
		if hasTarget {
			out = append(out, r)
		}
	}
	return out
}

func parseSuccessCriteria(n *yaml.Node) []model.SuccessCriterion {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]model.SuccessCriterion, 0, len(n.Content))
	for _, item := range n.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		c := model.SuccessCriterion{}
		hasCondition := false
		for _, kv := range mappingPairs(item) {
			switch kv.Key {
			case "condition":
				c.Condition = scalarString(kv.Value)
				hasCondition = true
			case "context":
				c.Context = scalarString(kv.Value)
			case "version":
				// The official JSON Schema validates the Criterion
				// Expression Type Object flat: `version` next to `type`.
				c.TypeVersion = scalarString(kv.Value)
			case "type":
				// The spec prose allows `type` to be the Criterion
				// Expression Type Object itself ({type, version}); accept
				// both that nested form and the plain scalar.
				if kv.Value != nil && kv.Value.Kind == yaml.MappingNode {
					for _, typeKV := range mappingPairs(kv.Value) {
						switch typeKV.Key {
						case "type":
							c.Type = scalarString(typeKV.Value)
						case "version":
							c.TypeVersion = scalarString(typeKV.Value)
						}
					}
				} else {
					c.Type = scalarString(kv.Value)
				}
			}
		}
		// Presence of the required `condition` key decides, not its value:
		// an empty condition is an authoring mistake that must stay visible
		// downstream (render, generated comments) instead of vanishing.
		if hasCondition {
			out = append(out, c)
		}
	}
	return out
}

// parseOutputs preserves the declaration order of an `outputs` mapping.
func parseOutputs(n *yaml.Node) []model.OutputEntry {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := make([]model.OutputEntry, 0, len(n.Content)/2)
	for _, kv := range mappingPairs(n) {
		out = append(out, model.OutputEntry{
			Name:       kv.Key,
			Expression: scalarString(kv.Value),
		})
	}
	return out
}
