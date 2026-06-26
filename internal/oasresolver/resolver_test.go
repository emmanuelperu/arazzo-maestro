package oasresolver

import (
	"os"
	"path/filepath"
	"testing"
)

const basicSpec = `
openapi: "3.1.0"
info: { title: Basic, version: "1.0.0" }
servers:
  - url: https://api.example.com
paths:
  /users:
    get:
      operationId: listUsers
      responses: { "200": { description: ok } }
    post:
      operationId: createUser
      responses: { "201": { description: created } }
  /users/{id}:
    get:
      operationId: getUser
      responses: { "200": { description: ok } }
    delete:
      operationId: deleteUser
      responses: { "204": { description: ok } }
`

func writeSpec(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return path
}

func loadBasic(t *testing.T) *Source {
	t.Helper()
	src, err := Load(writeSpec(t, basicSpec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return src
}

func TestResolveBasic(t *testing.T) {
	src := loadBasic(t)
	cases := []struct {
		opID, method, path string
	}{
		{"listUsers", "GET", "/users"},
		{"createUser", "POST", "/users"},
		{"getUser", "GET", "/users/{id}"},
		{"deleteUser", "DELETE", "/users/{id}"},
	}
	for _, tc := range cases {
		t.Run(tc.opID, func(t *testing.T) {
			op, err := src.Resolve(tc.opID)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tc.opID, err)
			}
			if op.Method != tc.method {
				t.Errorf("Method = %q, want %q", op.Method, tc.method)
			}
			if op.Path != tc.path {
				t.Errorf("Path = %q, want %q", op.Path, tc.path)
			}
			if op.BaseURL != "https://api.example.com" {
				t.Errorf("BaseURL = %q, want doc-level server", op.BaseURL)
			}
			if op.Spec == nil {
				t.Fatal("Spec is nil")
			}
			if op.Spec.OperationId != tc.opID {
				t.Errorf("Spec.OperationId = %q, want %q", op.Spec.OperationId, tc.opID)
			}
		})
	}
}

func TestResolveUnknownOperationID(t *testing.T) {
	src := loadBasic(t)
	if _, err := src.Resolve("noSuchOp"); err == nil {
		t.Fatal("expected error for unknown operationId, got nil")
	}
}

func TestHasOperationID(t *testing.T) {
	src := loadBasic(t)
	if !src.HasOperationID("listUsers") {
		t.Error("HasOperationID(listUsers) = false, want true")
	}
	if src.HasOperationID("noSuchOp") {
		t.Error("HasOperationID(noSuchOp) = true, want false")
	}
}

func TestOperationIDs(t *testing.T) {
	src := loadBasic(t)
	got := src.OperationIDs()
	want := []string{"listUsers", "createUser", "getUser", "deleteUser"}
	if len(got) != len(want) {
		t.Errorf("len = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for _, id := range want {
		if !got[id] {
			t.Errorf("OperationIDs missing %q", id)
		}
	}
}

func TestServerURLFallback(t *testing.T) {
	const spec = `
openapi: "3.1.0"
info: { title: Fallback, version: "1.0.0" }
servers:
  - url: https://doc.example.com
paths:
  /inherits-doc:
    get:
      operationId: docLevel
      responses: { "200": { description: ok } }
  /has-path-server:
    servers:
      - url: https://path.example.com
    get:
      operationId: pathLevel
      responses: { "200": { description: ok } }
    post:
      operationId: opLevel
      servers:
        - url: https://op.example.com
      responses: { "200": { description: ok } }
`
	src, err := Load(writeSpec(t, spec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := []struct {
		opID, wantBaseURL string
	}{
		{"docLevel", "https://doc.example.com"},
		{"pathLevel", "https://path.example.com"},
		{"opLevel", "https://op.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.opID, func(t *testing.T) {
			op, err := src.Resolve(tc.opID)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if op.BaseURL != tc.wantBaseURL {
				t.Errorf("BaseURL = %q, want %q", op.BaseURL, tc.wantBaseURL)
			}
		})
	}
}

func TestServerURLEmptyWhenNoServersDeclared(t *testing.T) {
	const spec = `
openapi: "3.1.0"
info: { title: NoServers, version: "1.0.0" }
paths:
  /ping:
    get:
      operationId: ping
      responses: { "200": { description: ok } }
`
	src, err := Load(writeSpec(t, spec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	op, err := src.Resolve("ping")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if op.BaseURL != "" {
		t.Errorf("BaseURL = %q, want empty string when no servers declared anywhere", op.BaseURL)
	}
}

func TestDuplicateOperationIDFirstWins(t *testing.T) {
	const spec = `
openapi: "3.1.0"
info: { title: Dup, version: "1.0.0" }
paths:
  /first:
    get:
      operationId: dup
      responses: { "200": { description: ok } }
  /second:
    post:
      operationId: dup
      responses: { "200": { description: ok } }
`
	src, err := Load(writeSpec(t, spec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	op, err := src.Resolve("dup")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if op.Method != "GET" || op.Path != "/first" {
		t.Errorf("first-wins broken: got %s %s, want GET /first", op.Method, op.Path)
	}
}

func TestLoadFileNotFound(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	if _, err := Load(writeSpec(t, "this is: not [valid yaml")); err == nil {
		t.Fatal("expected error for malformed input, got nil")
	}
}

func TestLoadRejectsSwagger2(t *testing.T) {
	const swagger = `
swagger: "2.0"
info: { title: Old, version: "1.0.0" }
paths:
  /ping:
    get:
      operationId: ping
      responses: { "200": { description: ok } }
`
	if _, err := Load(writeSpec(t, swagger)); err == nil {
		t.Fatal("expected error when source is Swagger 2.0, got nil")
	}
}

func TestLoadHandlesDocumentWithoutPaths(t *testing.T) {
	const spec = `
openapi: "3.1.0"
info: { title: NoPaths, version: "1.0.0" }
components:
  schemas:
    Empty: { type: object }
`
	src, err := Load(writeSpec(t, spec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(src.OperationIDs()); got != 0 {
		t.Errorf("OperationIDs len = %d, want 0 for a doc without paths", got)
	}
	if src.HasOperationID("anything") {
		t.Error("HasOperationID returned true on a paths-less doc")
	}
}

func TestAllHTTPMethodsAreIndexed(t *testing.T) {
	const spec = `
openapi: "3.1.0"
info: { title: AllVerbs, version: "1.0.0" }
paths:
  /r:
    get:     { operationId: opGet,     responses: { "200": { description: ok } } }
    put:     { operationId: opPut,     responses: { "200": { description: ok } } }
    post:    { operationId: opPost,    responses: { "200": { description: ok } } }
    delete:  { operationId: opDelete,  responses: { "200": { description: ok } } }
    options: { operationId: opOptions, responses: { "200": { description: ok } } }
    head:    { operationId: opHead,    responses: { "200": { description: ok } } }
    patch:   { operationId: opPatch,   responses: { "200": { description: ok } } }
    trace:   { operationId: opTrace,   responses: { "200": { description: ok } } }
`
	src, err := Load(writeSpec(t, spec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cases := []struct {
		opID, method string
	}{
		{"opGet", "GET"},
		{"opPut", "PUT"},
		{"opPost", "POST"},
		{"opDelete", "DELETE"},
		{"opOptions", "OPTIONS"},
		{"opHead", "HEAD"},
		{"opPatch", "PATCH"},
		{"opTrace", "TRACE"},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			op, err := src.Resolve(tc.opID)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tc.opID, err)
			}
			if op.Method != tc.method {
				t.Errorf("Method = %q, want %q", op.Method, tc.method)
			}
		})
	}
}

func TestQueryVerbIsIndexed(t *testing.T) {
	// QUERY is a HTTP verb introduced in OpenAPI 3.2. It is part of the
	// methodOps slice and needs end-to-end coverage to confirm
	// libopenapi exposes it under PathItem.Query as expected.
	const spec = `
openapi: "3.2.0"
info: { title: Query, version: "1.0.0" }
paths:
  /search:
    query:
      operationId: opQuery
      responses: { "200": { description: ok } }
`
	src, err := Load(writeSpec(t, spec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	op, err := src.Resolve("opQuery")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if op.Method != "QUERY" {
		t.Errorf("Method = %q, want %q", op.Method, "QUERY")
	}
}

func TestOperationsWithoutOperationIDAreSkipped(t *testing.T) {
	const spec = `
openapi: "3.1.0"
info: { title: NoOpID, version: "1.0.0" }
paths:
  /mixed:
    get:
      operationId: namedOp
      responses: { "200": { description: ok } }
    post:
      operationId: ""
      responses: { "200": { description: ok } }
    delete:
      responses: { "200": { description: ok } }
`
	src, err := Load(writeSpec(t, spec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ids := src.OperationIDs()
	if len(ids) != 1 || !ids["namedOp"] {
		t.Errorf("OperationIDs = %v, want only {namedOp}", ids)
	}
}

func TestPathItemWithSharedParametersIsHandled(t *testing.T) {
	// `parameters` and `summary` at the path level are non-operation
	// fields; they used to break the previous hand-rolled YAML decoder
	// in the linter (see git history). Make sure the resolver does not
	// regress on them.
	const spec = `
openapi: "3.1.0"
info: { title: Shared, version: "1.0.0" }
paths:
  /items/{id}:
    summary: Item operations
    parameters:
      - name: id
        in: path
        required: true
        schema: { type: string }
    get:
      operationId: getItem
      responses: { "200": { description: ok } }
    delete:
      operationId: deleteItem
      responses: { "204": { description: ok } }
`
	src, err := Load(writeSpec(t, spec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, id := range []string{"getItem", "deleteItem"} {
		op, err := src.Resolve(id)
		if err != nil {
			t.Errorf("Resolve(%q): %v", id, err)
			continue
		}
		if op.Path != "/items/{id}" {
			t.Errorf("Path = %q, want %q", op.Path, "/items/{id}")
		}
	}
}

func TestResolveExposesFullSpecWithResolvedRefs(t *testing.T) {
	// This test verifies the actual reason Operation carries Spec:
	// callers should reach into Parameters, RequestBody and Responses
	// directly through libopenapi types. It also asserts that local
	// `$ref` chains are followed by libopenapi, which is the main
	// reason for adopting it over hand-rolled parsing.
	const spec = `
openapi: "3.1.0"
info: { title: Refs, version: "1.0.0" }
paths:
  /users:
    post:
      operationId: createUser
      parameters:
        - $ref: '#/components/parameters/TraceID'
      requestBody:
        $ref: '#/components/requestBodies/CreateUserBody'
      responses:
        "201":
          $ref: '#/components/responses/Created'
components:
  parameters:
    TraceID:
      name: X-Trace-Id
      in: header
      schema: { type: string }
  requestBodies:
    CreateUserBody:
      required: true
      content:
        application/json:
          schema: { type: object }
  responses:
    Created:
      description: created
`
	src, err := Load(writeSpec(t, spec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	op, err := src.Resolve("createUser")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if op.Spec == nil {
		t.Fatal("Spec is nil")
	}

	if got := len(op.Spec.Parameters); got != 1 {
		t.Fatalf("Spec.Parameters len = %d, want 1", got)
	}
	if name := op.Spec.Parameters[0].Name; name != "X-Trace-Id" {
		t.Errorf("Spec.Parameters[0].Name = %q, want %q (ref not resolved?)", name, "X-Trace-Id")
	}
	if in := op.Spec.Parameters[0].In; in != "header" {
		t.Errorf("Spec.Parameters[0].In = %q, want %q", in, "header")
	}

	if op.Spec.RequestBody == nil {
		t.Fatal("Spec.RequestBody is nil (ref not resolved?)")
	}
	if op.Spec.RequestBody.Required == nil || !*op.Spec.RequestBody.Required {
		t.Errorf("Spec.RequestBody.Required = %v, want true", op.Spec.RequestBody.Required)
	}
	if op.Spec.RequestBody.Content == nil {
		t.Fatal("Spec.RequestBody.Content is nil")
	}
	if _, ok := op.Spec.RequestBody.Content.Get("application/json"); !ok {
		t.Error("Spec.RequestBody.Content has no application/json entry")
	}

	if op.Spec.Responses == nil || op.Spec.Responses.Codes == nil {
		t.Fatal("Spec.Responses or Responses.Codes is nil")
	}
	created, ok := op.Spec.Responses.Codes.Get("201")
	if !ok {
		t.Fatal("Spec.Responses has no 201 entry")
	}
	if created.Description != "created" {
		t.Errorf("Spec.Responses[201].Description = %q, want %q (ref not resolved?)", created.Description, "created")
	}
}

func TestEffectiveContentType(t *testing.T) {
	cases := []struct {
		name     string
		explicit string
		declared []string
		want     string
		wantOK   bool
	}{
		{"explicit wins", "application/xml", []string{"application/json"}, "application/xml", true},
		{"single declared", "", []string{"application/json"}, "application/json", true},
		{"json among several", "", []string{"text/plain", "application/json"}, "application/json", true},
		{"ambiguous non-json", "", []string{"text/plain", "application/xml"}, "", false},
		{"none declared", "", nil, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := EffectiveContentType(c.explicit, c.declared)
			if got != c.want || ok != c.wantOK {
				t.Errorf("EffectiveContentType(%q, %v) = (%q, %v), want (%q, %v)",
					c.explicit, c.declared, got, ok, c.want, c.wantOK)
			}
		})
	}
}

func TestEffectiveContentTypeJSONSuffixAmongSeveral(t *testing.T) {
	for _, declared := range [][]string{
		{"text/plain", "application/vnd.api+json"},
		{"multipart/form-data", "application/json; charset=utf-8"},
	} {
		got, ok := EffectiveContentType("", declared)
		if !ok || !isJSONMediaType(got) {
			t.Errorf("EffectiveContentType(\"\", %v) = (%q, %v), want a JSON media type", declared, got, ok)
		}
	}
}
