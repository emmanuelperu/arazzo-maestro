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
