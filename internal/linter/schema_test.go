package linter

import (
	"strings"
	"testing"
)

// minimalValidDoc is the smallest Arazzo document that satisfies the
// JSON Schema (arazzo + info + sourceDescriptions + workflows, with
// required sub-fields).
const minimalValidDoc = `
arazzo: "1.0.1"
info:
  title: Minimal
  version: "1.0.0"
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: only
    steps:
      - stepId: ping
        operationId: ping
        successCriteria:
          - condition: $statusCode == 200
`

func TestSchemaAcceptsMinimalDoc(t *testing.T) {
	issues := lintSchema([]byte(minimalValidDoc))
	if len(issues) > 0 {
		t.Errorf("expected no schema issues on minimal valid doc, got %v", issues)
	}
}

func TestSchemaAccepts11VersionAfterPatch(t *testing.T) {
	// The published schema only allows 1.0.x; our load-time patch
	// extends acceptance to 1.1.x. This test guards that patch.
	doc := strings.Replace(minimalValidDoc, `arazzo: "1.0.1"`, `arazzo: "1.1.0"`, 1)
	issues := lintSchema([]byte(doc))
	for _, issue := range issues {
		if strings.Contains(issue.Path, "arazzo") {
			t.Errorf("1.1.0 should be accepted, got %s", issue)
		}
	}
}

func TestSchemaAccepts11StructuralFeatures(t *testing.T) {
	// Spec 1.1.0 structural additions grafted onto the 1.0 schema at
	// load time: each of these documents is spec-valid 1.1 and must
	// lint clean.
	tests := []struct {
		name string
		doc  string
	}{
		{
			name: "$self at root",
			doc: strings.Replace(minimalValidDoc, `arazzo: "1.0.1"`,
				"arazzo: \"1.1.0\"\n$self: https://example.com/checkout.arazzo.yaml", 1),
		},
		{
			name: "asyncapi source description",
			doc: strings.Replace(minimalValidDoc, "type: openapi",
				"type: asyncapi", 1),
		},
		{
			name: "querystring parameter location",
			doc: strings.Replace(minimalValidDoc, "operationId: ping",
				"operationId: ping\n        parameters:\n          - name: q\n            in: querystring\n            value: \"a=1&b=2\"", 1),
		},
		{
			name: "channelPath step",
			doc: strings.Replace(minimalValidDoc, "operationId: ping",
				"channelPath: $sourceDescriptions.api#/channels/orders", 1),
		},
		{
			// The official schema models the Expression Type Object
			// inline: type and version are criterion-level siblings.
			name: "jsonpath rfc9535 expression version",
			doc: strings.Replace(minimalValidDoc, "- condition: $statusCode == 200",
				"- condition: $.status\n            context: $response.body\n            type: jsonpath\n            version: rfc9535", 1),
		},
		{
			name: "jsonpointer expression type",
			doc: strings.Replace(minimalValidDoc, "- condition: $statusCode == 200",
				"- condition: /status\n            context: $response.body\n            type: jsonpointer\n            version: rfc6901", 1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, issue := range lintSchema([]byte(tt.doc)) {
				t.Errorf("unexpected schema issue: %s", issue)
			}
		})
	}
}

func TestSchemaStillRejectsInvalid11Lookalikes(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "unknown root key",
			doc: strings.Replace(minimalValidDoc, `arazzo: "1.0.1"`,
				"arazzo: \"1.1.0\"\n$selff: typo", 1),
			want: "not allowed",
		},
		{
			name: "bogus jsonpointer version",
			doc: strings.Replace(minimalValidDoc, "- condition: $statusCode == 200",
				"- condition: /status\n            context: $response.body\n            type: jsonpointer\n            version: rfc0000", 1),
			want: "value must be",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := lintSchema([]byte(tt.doc))
			if !containsMessage(issues, tt.want) {
				t.Errorf("expected issue containing %q, got %v", tt.want, issues)
			}
		})
	}
}

func TestSchemaRejectsMissingRequiredField(t *testing.T) {
	// Omit the top-level `info`.
	src := `
arazzo: "1.0.1"
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: only
    steps:
      - stepId: ping
        operationId: ping
        successCriteria:
          - condition: $statusCode == 200
`
	issues := lintSchema([]byte(src))
	if !containsMessage(issues, "info") {
		t.Errorf("expected missing-info issue, got %v", issues)
	}
}

func TestSchemaRejectsWrongType(t *testing.T) {
	// `workflows` should be an array; pass a string instead.
	src := `
arazzo: "1.0.1"
info:
  title: x
  version: "1.0.0"
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
workflows: "not-an-array"
`
	issues := lintSchema([]byte(src))
	if !containsMessage(issues, "expected array") {
		t.Errorf("expected type-mismatch issue, got %v", issues)
	}
}

func TestSchemaRejectsBadVersionPattern(t *testing.T) {
	doc := strings.Replace(minimalValidDoc, `arazzo: "1.0.1"`, `arazzo: "2.0.0"`, 1)
	issues := lintSchema([]byte(doc))
	if !containsMessage(issues, "pattern") {
		t.Errorf("expected version-pattern issue, got %v", issues)
	}
}

func TestSchemaJSONPointerToHumanPath(t *testing.T) {
	cases := []struct{ ptr, want string }{
		{"", "<root>"},
		{"/workflows/0/steps/2/operationId", "workflows[0].steps[2].operationId"},
		{"/info/title", "info.title"},
		{"/paths/~1products/get", "paths./products.get"},
	}
	for _, c := range cases {
		if got := jsonPointerToPath(c.ptr); got != c.want {
			t.Errorf("jsonPointerToPath(%q) = %q, want %q", c.ptr, got, c.want)
		}
	}
}
