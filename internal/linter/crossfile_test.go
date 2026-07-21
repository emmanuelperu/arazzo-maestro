package linter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/emmanuelperu/arazzo-maestro/internal/parser"
)

const tinyOpenAPI = `
openapi: "3.1.0"
info: { title: Tiny, version: "1.0.0" }
paths:
  /ping:
    get:
      operationId: ping
      responses: { "200": { description: ok } }
  /pong:
    post:
      operationId: pong
      responses: { "200": { description: ok } }
`

const arazzoSingleSource = `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        operationId: ping
        successCriteria:
          - condition: $statusCode == 200
`

func setupArazzoProject(t *testing.T, arazzo, openapi string) (arazzoPath, basePath string) {
	t.Helper()
	dir := t.TempDir()
	arazzoPath = filepath.Join(dir, "shop.yaml")
	if err := os.WriteFile(arazzoPath, []byte(arazzo), 0o644); err != nil {
		t.Fatal(err)
	}
	if openapi != "" {
		if err := os.WriteFile(filepath.Join(dir, "openapi.yaml"), []byte(openapi), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return arazzoPath, dir
}

// Regression: an OpenAPI 3.0 Path Item may declare shared `parameters`
// (and summary/description/servers) alongside its operations. The linter
// must still extract the operationIds, not choke on the non-operation
// fields (an earlier version aborted the whole parse on the `parameters`
// sequence).
const openAPIWithPathLevelParams = `
openapi: "3.0.3"
info: { title: Tiny, version: "1.0.0" }
paths:
  /products/{productId}:
    summary: A single product
    description: Operations on one product.
    parameters:
      - name: productId
        in: path
        required: true
        schema: { type: string, pattern: '^p-[a-zA-Z0-9]+$' }
    get:
      operationId: getProduct
      responses: { "200": { description: ok } }
`

func TestCrossFileAcceptsPathLevelSharedParameters(t *testing.T) {
	src := `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        operationId: getProduct
        successCriteria:
          - condition: $statusCode == 200
`
	_, base := setupArazzoProject(t, src, openAPIWithPathLevelParams)
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintCrossFile(doc, base)
	for _, issue := range issues {
		t.Errorf("unexpected finding (path-level parameters must be tolerated): %s", issue)
	}
}

func TestCrossFileAcceptsValidReference(t *testing.T) {
	_, base := setupArazzoProject(t, arazzoSingleSource, tinyOpenAPI)
	doc, _ := parser.ParseBytes([]byte(arazzoSingleSource))
	issues := lintCrossFile(doc, base)
	for _, issue := range issues {
		t.Errorf("unexpected finding: %s", issue)
	}
}

func TestCrossFileRejectsUnknownOperation(t *testing.T) {
	src := `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        operationId: nope
`
	_, base := setupArazzoProject(t, src, tinyOpenAPI)
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintCrossFile(doc, base)
	if !containsMessage(issues, `operation "nope" not found`) {
		t.Errorf("expected unknown-operation issue, got %v", issues)
	}
}

func TestCrossFileRejectsMissingSourceFile(t *testing.T) {
	_, base := setupArazzoProject(t, arazzoSingleSource, "") // openapi.yaml not written
	doc, _ := parser.ParseBytes([]byte(arazzoSingleSource))
	issues := lintCrossFile(doc, base)
	if !containsMessage(issues, "file not found") {
		t.Errorf("expected file-not-found issue, got %v", issues)
	}
}

// When a source can't be loaded, step references against it should NOT
// produce a secondary "source not declared" message, the source error
// already informed the user. Regression for an earlier bug where an
// empty operation index produced sourceName="" downstream.
func TestCrossFileDoesNotReportRedundantSourceErrors(t *testing.T) {
	_, base := setupArazzoProject(t, arazzoSingleSource, "") // openapi.yaml not written
	doc, _ := parser.ParseBytes([]byte(arazzoSingleSource))
	issues := lintCrossFile(doc, base)
	for _, issue := range issues {
		if containsMessage([]Issue{issue}, `source "" is not declared`) {
			t.Errorf("unexpected secondary error: %s", issue)
		}
		if containsMessage([]Issue{issue}, `operation "ping" not found`) {
			t.Errorf("should not report missing op when source is unloaded: %s", issue)
		}
	}
}

func TestCrossFileRejectsHTTPSURL(t *testing.T) {
	src := `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: https://api.example.com/openapi.yaml
    type: openapi
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        operationId: ping
`
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintCrossFile(doc, t.TempDir())
	if !containsMessage(issues, "HTTP source URLs are not supported") {
		t.Errorf("expected HTTPS rejection, got %v", issues)
	}
}

func TestCrossFileRequiresQualifiedFormWithMultipleSources(t *testing.T) {
	src := `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
  - name: other
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        operationId: ping
`
	_, base := setupArazzoProject(t, src, tinyOpenAPI)
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintCrossFile(doc, base)
	if !containsMessage(issues, "unqualified operationId") {
		t.Errorf("expected qualified-form requirement, got %v", issues)
	}
}

func TestCrossFileAcceptsQualifiedForm(t *testing.T) {
	src := `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
  - name: other
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        operationId: $sourceDescriptions.api.ping
`
	_, base := setupArazzoProject(t, src, tinyOpenAPI)
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintCrossFile(doc, base)
	for _, issue := range issues {
		t.Errorf("unexpected finding: %s", issue)
	}
}

func TestParseOperationRef(t *testing.T) {
	cases := []struct {
		ref       string
		source    string
		opID      string
		qualified bool
	}{
		{"ping", "", "ping", false},
		{"$sourceDescriptions.api.ping", "api", "ping", true},
		{"$sourceDescriptions.shop-api.list-products", "shop-api", "list-products", true},
	}
	for _, c := range cases {
		s, op, q := parseOperationRef(c.ref)
		if s != c.source || op != c.opID || q != c.qualified {
			t.Errorf("parseOperationRef(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.ref, s, op, q, c.source, c.opID, c.qualified)
		}
	}
}

func TestCrossFileResolvesOperationPath(t *testing.T) {
	src := `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        operationPath: '{$sourceDescriptions.api.url}#/paths/~1ping/get'
        successCriteria:
          - condition: $statusCode == 200
`
	_, base := setupArazzoProject(t, src, tinyOpenAPI)
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintCrossFile(doc, base)
	for _, issue := range issues {
		t.Errorf("unexpected finding: %s", issue)
	}
}

func TestCrossFileRejectsBadOperationPaths(t *testing.T) {
	cases := []struct {
		name, ref, wantMsg string
	}{
		{
			name:    "malformed reference",
			ref:     "$sourceDescriptions.api.url#/paths/~1ping/get",
			wantMsg: "malformed operationPath",
		},
		{
			name:    "unknown source",
			ref:     "{$sourceDescriptions.ghost.url}#/paths/~1ping/get",
			wantMsg: `source description "ghost" which does not exist`,
		},
		{
			name:    "pointer not an operation",
			ref:     "{$sourceDescriptions.api.url}#/components/schemas/Pet",
			wantMsg: "does not address an operation",
		},
		{
			name:    "operation not in source",
			ref:     "{$sourceDescriptions.api.url}#/paths/~1ping/post",
			wantMsg: `no POST operation on path "/ping" in source "api"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        operationPath: '` + tc.ref + `'
        successCriteria:
          - condition: $statusCode == 200
`
			_, base := setupArazzoProject(t, src, tinyOpenAPI)
			doc, _ := parser.ParseBytes([]byte(src))
			issues := lintCrossFile(doc, base)
			if !containsMessage(issues, tc.wantMsg) {
				t.Errorf("expected an issue containing %q, got %v", tc.wantMsg, issues)
			}
		})
	}
}

func TestCrossFileRejectsOperationRefsToNonOpenAPISource(t *testing.T) {
	// LoadAll skips non-openapi sources without a SourceResult, so
	// nothing else reports these references; the generators would emit
	// an __unresolved__ placeholder for both steps.
	src := `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
  - name: other
    url: ./other.arazzo.yaml
    type: arazzo
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        operationPath: '{$sourceDescriptions.other.url}#/paths/~1x/get'
      - stepId: b
        operationId: $sourceDescriptions.other.getX
`
	_, base := setupArazzoProject(t, src, tinyOpenAPI)
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintCrossFile(doc, base)
	if !containsMessage(issues, `operationPath references source "other" of type "arazzo"`) {
		t.Errorf("expected non-openapi operationPath issue, got %v", issues)
	}
	if !containsMessage(issues, `operationId references source "other" of type "arazzo"`) {
		t.Errorf("expected non-openapi operationId issue, got %v", issues)
	}
}

func TestCrossFileRejectsQualifiedOperationIDOnUnknownSource(t *testing.T) {
	src := `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        operationId: $sourceDescriptions.ghost.ping
`
	_, base := setupArazzoProject(t, src, tinyOpenAPI)
	doc, _ := parser.ParseBytes([]byte(src))
	issues := lintCrossFile(doc, base)
	if !containsMessage(issues, `operationId references source description "ghost" which does not exist`) {
		t.Errorf("expected unknown-source issue, got %v", issues)
	}
}
