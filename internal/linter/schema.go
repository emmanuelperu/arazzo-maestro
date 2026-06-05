// Schema validation pass, runs the official Arazzo JSON Schema against
// the raw YAML document. This catches the bulk of structural errors
// (types, required fields, enums, formats) without us having to
// hand-roll equivalent Go rules.
//
// The embedded schema is the official Arazzo 1.0 schema published by
// the OAI at https://spec.openapis.org/arazzo/1.0/schema/2025-10-15.
// At load time we loosen its `arazzo` version pattern so that both
// 1.0.x and 1.1.x documents validate. Rationale:
//   - The OAI has published spec 1.1.0 (May 2026) but no 1.1 JSON
//     schema yet (the published schema is explicitly labelled
//     "non-authoritative").
//   - The 1.1 release is documentation-focused; the structure the
//     schema validates is unchanged for the parts we care about.
// When OAI publishes the 1.1 schema, we'll embed it and drop the patch.

package linter

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

//go:embed schemas/arazzo-1.0.json
var arazzoSchemaJSON []byte

// patchedArazzoVersionPattern accepts 1.0.x and 1.1.x. The original
// schema only allows ^1\.0\.\d+(-.+)?$.
const patchedArazzoVersionPattern = `^1\.[01]\.\d+(-.+)?$`

var (
	compiledSchema     *jsonschema.Schema
	compiledSchemaOnce sync.Once
	compiledSchemaErr  error
)

func loadArazzoSchema() (*jsonschema.Schema, error) {
	compiledSchemaOnce.Do(func() {
		patched, err := patchSchemaVersionPattern(arazzoSchemaJSON)
		if err != nil {
			compiledSchemaErr = fmt.Errorf("patching arazzo schema: %w", err)
			return
		}
		compiler := jsonschema.NewCompiler()
		const url = "https://spec.openapis.org/arazzo/1.0/schema/2025-10-15"
		if err := compiler.AddResource(url, strings.NewReader(string(patched))); err != nil {
			compiledSchemaErr = fmt.Errorf("adding arazzo schema: %w", err)
			return
		}
		s, err := compiler.Compile(url)
		if err != nil {
			compiledSchemaErr = fmt.Errorf("compiling arazzo schema: %w", err)
			return
		}
		compiledSchema = s
	})
	return compiledSchema, compiledSchemaErr
}

// patchSchemaVersionPattern relaxes the `arazzo` field pattern so 1.1.x
// documents validate. See file header for rationale.
func patchSchemaVersionPattern(raw []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	props, _ := doc["properties"].(map[string]any)
	arazzo, _ := props["arazzo"].(map[string]any)
	if arazzo == nil {
		return nil, fmt.Errorf("schema is missing properties.arazzo")
	}
	arazzo["pattern"] = patchedArazzoVersionPattern
	return json.Marshal(doc)
}

func lintSchema(raw []byte) []Issue {
	if len(raw) == 0 {
		return nil
	}
	schema, err := loadArazzoSchema()
	if err != nil {
		return []Issue{{
			Severity: SeverityError,
			Path:     "<schema>",
			Message:  err.Error(),
		}}
	}
	instance, err := yamlToJSONValue(raw)
	if err != nil {
		return []Issue{{
			Severity: SeverityError,
			Path:     "<document>",
			Message:  fmt.Sprintf("invalid YAML: %s", err),
		}}
	}
	if err := schema.Validate(instance); err != nil {
		return collectSchemaIssues(err)
	}
	return nil
}

// yamlToJSONValue decodes a YAML document into a JSON-compatible
// interface{} tree. yaml.v3 unmarshals mappings into map[string]any
// (unlike v2 which used map[interface{}]interface{}), so the result is
// directly accepted by jsonschema.Schema.Validate.
func yamlToJSONValue(raw []byte) (any, error) {
	var v any
	if err := yaml.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func collectSchemaIssues(err error) []Issue {
	var root *jsonschema.ValidationError
	if !errors.As(err, &root) {
		return []Issue{{Severity: SeverityError, Path: "<schema>", Message: err.Error()}}
	}
	leaves := collectLeaves(root)
	seen := make(map[string]bool, len(leaves))
	out := make([]Issue, 0, len(leaves))
	for _, leaf := range leaves {
		path := jsonPointerToPath(leaf.InstanceLocation)
		msg := humanMessage(leaf)
		key := path + "|" + msg
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Issue{
			Severity: SeverityError,
			Path:     path,
			Message:  msg,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// collectLeaves walks the ValidationError tree and returns the deepest
// nodes. Intermediate nodes carry redundant "doesn't match X" wrappers
// that don't help the user.
func collectLeaves(e *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if len(e.Causes) == 0 {
		return []*jsonschema.ValidationError{e}
	}
	var out []*jsonschema.ValidationError
	for _, c := range e.Causes {
		out = append(out, collectLeaves(c)...)
	}
	return out
}

// jsonPointerToPath turns a JSON Pointer like
// "/workflows/0/steps/2/operationId" into the user-friendly
// "workflows[0].steps[2].operationId".
func jsonPointerToPath(ptr string) string {
	if ptr == "" || ptr == "/" {
		return "<root>"
	}
	parts := strings.Split(strings.TrimPrefix(ptr, "/"), "/")
	var b strings.Builder
	for _, p := range parts {
		p = strings.ReplaceAll(p, "~1", "/")
		p = strings.ReplaceAll(p, "~0", "~")
		if isAllDigits(p) {
			b.WriteString("[")
			b.WriteString(p)
			b.WriteString("]")
			continue
		}
		if b.Len() > 0 {
			b.WriteString(".")
		}
		b.WriteString(p)
	}
	return b.String()
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// humanMessage rewrites a few jsonschema messages into friendlier wording.
func humanMessage(e *jsonschema.ValidationError) string {
	msg := e.Message
	switch {
	case strings.HasPrefix(msg, "missing properties:"):
		return "missing required field" + strings.TrimPrefix(msg, "missing properties:")
	case strings.HasPrefix(msg, "does not match pattern"):
		return strings.Replace(msg, "does not match pattern", "value does not match expected pattern", 1)
	case strings.HasPrefix(msg, "expected"):
		return msg // "expected string, but got integer" is already clear
	}
	return msg
}
