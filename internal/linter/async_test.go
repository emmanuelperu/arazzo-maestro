package linter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
	"github.com/emmanuelperu/arazzo-maestro/internal/parser"
)

// asyncDoc mirrors the Arazzo 1.1 spec's async step example (Step Object
// Examples): an HTTP call, a send and a receive joined by dependsOn.
const asyncDoc = `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
  - name: events
    url: ./events.asyncapi.yaml
    type: asyncapi
workflows:
  - workflowId: wf
    inputs:
      type: object
      properties:
        correlationId: { type: string }
    steps:
      - stepId: ping
        operationId: $sourceDescriptions.api.ping
        successCriteria:
          - condition: $statusCode == 200
      - stepId: placeOrder
        operationId: $sourceDescriptions.events.placeOrder
        action: send
        requestBody:
          payload:
            productId: 42
      - stepId: confirmOrder
        channelPath: '{$sourceDescriptions.events.url}#/channels/orderConfirmed'
        action: receive
        correlationId: $inputs.correlationId
        timeout: 6000
        dependsOn:
          - placeOrder
        outputs:
          orderId: $message.payload#/orderId
`

func writeAsyncProject(t *testing.T, src string, withAsyncFile bool) string {
	t.Helper()
	path, dir := setupArazzoProject(t, src, tinyOpenAPI)
	if withAsyncFile {
		if err := os.WriteFile(filepath.Join(dir, "events.asyncapi.yaml"), []byte("asyncapi: 3.0.0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestLintAcceptsSpecAsyncSteps(t *testing.T) {
	issues, err := LintFile(writeAsyncProject(t, asyncDoc, true), oasresolver.DefaultRemoteOptions())
	if err != nil {
		t.Fatal(err)
	}
	warnings := 0
	for _, issue := range issues {
		if issue.Severity == SeverityError {
			t.Errorf("unexpected error: %s", issue)
			continue
		}
		warnings++
	}
	// The send's operationId and the receive's channelPath are accepted
	// but not resolved, and say so.
	if warnings != 2 || !containsMessage(issues, `"placeOrder" not checked`) || !containsMessage(issues, `"channel orderConfirmed" not checked`) {
		t.Errorf("expected the two not-resolved warnings, got %v", issues)
	}
}

func TestLintReportsMissingAsyncAPIFile(t *testing.T) {
	issues, err := LintFile(writeAsyncProject(t, asyncDoc, false), oasresolver.DefaultRemoteOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !containsMessage(issues, "file not found") {
		t.Errorf("expected a missing asyncapi file error, got %v", issues)
	}
}

func TestSchemaRejectsUnknownAction(t *testing.T) {
	doc := strings.Replace(asyncDoc, "action: send", "action: publish", 1)
	if issues := lintSchema([]byte(doc)); !containsMessage(issues, "value must be") {
		t.Errorf("expected an enum issue on action, got %v", issues)
	}
}

func TestCrossFileChecksChannelPath(t *testing.T) {
	tests := []struct {
		name, channelPath, want string
	}{
		{"not the spec form", "$sourceDescriptions.events#/channels/x", "malformed channelPath"},
		{"pointer outside channels", "{$sourceDescriptions.events.url}#/operations/x", "malformed channelPath"},
		{"empty channel", "{$sourceDescriptions.events.url}#/channels/", "malformed channelPath"},
		{"unknown source", "{$sourceDescriptions.nope.url}#/channels/x", `source description "nope" which does not exist`},
		{"openapi source", "{$sourceDescriptions.api.url}#/channels/x", `source "api" of type "openapi"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(asyncDoc, "'{$sourceDescriptions.events.url}#/channels/orderConfirmed'", "'"+tt.channelPath+"'", 1)
			issues, err := LintFile(writeAsyncProject(t, src, true), oasresolver.DefaultRemoteOptions())
			if err != nil {
				t.Fatal(err)
			}
			if !containsMessage(issues, tt.want) {
				t.Errorf("expected an issue containing %q, got %v", tt.want, issues)
			}
		})
	}
}

func TestSemanticChecksStepDependsOn(t *testing.T) {
	tests := []struct {
		name, dep, want string
	}{
		{"current workflow step", "placeOrder", ""},
		{"other workflow step", "$workflows.other.steps.prepare", ""},
		{"other document step", "$sourceDescriptions.events.wf.steps.x", ""},
		{"itself", "confirmOrder", "cannot depend on itself"},
		{"unknown step", "nope", `step "nope" does not exist in this workflow`},
		{"unknown workflow", "$workflows.nope.steps.prepare", `workflow "nope" does not exist`},
		{"unknown step in other workflow", "$workflows.other.steps.nope", `step "nope" does not exist in workflow "other"`},
		{"unknown source", "$sourceDescriptions.nope.wf.steps.x", `source description "nope" does not exist`},
		{"malformed workflows form", "$workflows.other", "malformed dependsOn"},
		{"other expression", "$inputs.x", "malformed dependsOn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := strings.Replace(asyncDoc, "          - placeOrder", "          - '"+tt.dep+"'", 1) +
				"  - workflowId: other\n    steps:\n      - stepId: prepare\n        operationId: $sourceDescriptions.api.ping\n"
			doc, err := parser.ParseBytes([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			issues := lintSemantic(doc)
			if tt.want == "" {
				for _, issue := range issues {
					t.Errorf("unexpected issue: %s", issue)
				}
				return
			}
			if !containsMessage(issues, tt.want) {
				t.Errorf("expected an issue containing %q, got %v", tt.want, issues)
			}
		})
	}
}

func TestSemanticWarnsOnMisplacedAsyncFields(t *testing.T) {
	tests := []struct {
		name, from, to, want string
	}{
		{"action on an openapi step", "operationId: $sourceDescriptions.api.ping", "operationId: $sourceDescriptions.api.ping\n        action: send", "action only applies to steps on an asyncapi source, not an openapi source"},
		{"correlationId on a send step", "action: send", "action: send\n        correlationId: $inputs.correlationId", "correlationId only applies to steps with action: receive"},
		{"correlationId referencing a later step", "correlationId: $inputs.correlationId", "correlationId: $steps.later.outputs.id", `"later"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := parser.ParseBytes([]byte(strings.Replace(asyncDoc, tt.from, tt.to, 1)))
			if err != nil {
				t.Fatal(err)
			}
			if issues := lintSemantic(doc); !containsMessage(issues, tt.want) {
				t.Errorf("expected an issue containing %q, got %v", tt.want, issues)
			}
		})
	}
}
