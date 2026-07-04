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
		}
	}
	return doc, nil
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
		case "inputs":
			wf.Inputs = parseInputs(kv.Value)
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
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var properties *yaml.Node
	for _, kv := range mappingPairs(n) {
		if kv.Key == "properties" {
			properties = kv.Value
			break
		}
	}
	if properties == nil || properties.Kind != yaml.MappingNode {
		return nil
	}
	out := make([]model.InputProperty, 0, len(properties.Content)/2)
	for _, kv := range mappingPairs(properties) {
		prop := model.InputProperty{Name: kv.Key}
		if kv.Value.Kind == yaml.MappingNode {
			for _, specKV := range mappingPairs(kv.Value) {
				switch specKV.Key {
				case "type":
					prop.Type = scalarString(specKV.Value)
				case "default":
					prop.Default = nodeToAny(specKV.Value)
				}
			}
		}
		out = append(out, prop)
	}
	return out
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
		a := model.SuccessAction{}
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
			}
		}
		out = append(out, a)
	}
	return out
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
		a := model.FailureAction{}
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
			}
		}
		out = append(out, a)
	}
	return out
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
		param := model.Parameter{}
		for _, kv := range mappingPairs(item) {
			switch kv.Key {
			case "name":
				param.Name = scalarString(kv.Value)
			case "in":
				param.In = scalarString(kv.Value)
			case "value":
				param.Value = nodeToAny(kv.Value)
			}
		}
		out = append(out, param)
	}
	return out
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
// Objects. Entries missing the required `target` are skipped (the schema
// pass reports them upstream).
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
		for _, kv := range mappingPairs(item) {
			switch kv.Key {
			case "target":
				r.Target = scalarString(kv.Value)
			case "value":
				r.Value = nodeToAny(kv.Value)
			}
		}
		if r.Target != "" {
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
		for _, kv := range mappingPairs(item) {
			if kv.Key == "condition" {
				out = append(out, model.SuccessCriterion{Condition: scalarString(kv.Value)})
			}
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
