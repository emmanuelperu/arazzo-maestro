//go:build integration

// Integration test that runs the real `hurl` binary against a generator
// output. The mock server, OpenAPI source, and Arazzo workflow are all
// built on the fly to keep the test self-contained.
//
// Run with: go test -tags=integration ./cmd/arazzo-maestro/
// Requires the hurl binary in PATH (install via `brew install hurl`).

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHurlRunsGeneratedFileAgainstMockServer(t *testing.T) {
	if _, err := exec.LookPath("hurl"); err != nil {
		t.Skip("hurl binary not in PATH; install with `brew install hurl`")
	}

	// Minimal mock server: list returns one widget, the second
	// endpoint returns the widget detail when the path id matches.
	// If hurl substitutes the captured variable correctly, the path
	// will be /widgets/w-42; any other path mismatches the handler
	// and the test will fail via http 404.
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/widgets":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]string{{"id": "w-42"}},
			})
		case r.Method == "GET" && r.URL.Path == "/widgets/w-42":
			_ = json.NewEncoder(w).Encode(map[string]string{
				"id": "w-42", "name": "Bolt",
			})
		default:
			t.Errorf("unexpected request: %s %s (chain broken?)", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer mock.Close()

	dir := t.TempDir()

	openapi := `openapi: "3.1.0"
info: { title: Widgets, version: "1.0.0" }
servers:
  - url: ` + mock.URL + `
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses: { "200": { description: ok } }
  /widgets/{id}:
    get:
      operationId: getWidget
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses: { "200": { description: ok } }
`
	writeFile(t, filepath.Join(dir, "openapi.yaml"), openapi)

	arazzo := `arazzo: "1.0.1"
info: { title: t, summary: chain, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: chain
    summary: list then fetch one
    steps:
      - stepId: list
        operationId: listWidgets
        outputs:
          firstId: $response.body#/items/0/id
        successCriteria:
          - condition: $statusCode == 200
      - stepId: get
        operationId: getWidget
        parameters:
          - name: id
            in: path
            value: $steps.list.outputs.firstId
        successCriteria:
          - condition: $statusCode == 200
`
	arazzoPath := filepath.Join(dir, "wf.arazzo.yaml")
	writeFile(t, arazzoPath, arazzo)

	outDir := filepath.Join(dir, "gen")
	stdout, _, err := runCmd(t, "test", "gen", "e2e", arazzoPath, "-o", outDir)
	if err != nil {
		t.Fatalf("test gen e2e failed: %v\n%s", err, stdout)
	}

	hurlFile := filepath.Join(outDir, "e2e", "hurl", "wf", "chain.hurl")
	if _, err := os.Stat(hurlFile); err != nil {
		t.Fatalf("expected generated file at %s: %v", hurlFile, err)
	}

	out, err := exec.Command("hurl", "--test", "--variable", "baseUrl="+mock.URL, hurlFile).CombinedOutput()
	if err != nil {
		body, _ := os.ReadFile(hurlFile)
		t.Fatalf("`hurl --test` failed: %v\n--- generated hurl ---\n%s\n--- hurl output ---\n%s",
			err, body, out)
	}
	if !strings.Contains(string(out), "Executed files") {
		t.Errorf("hurl --test output unexpected:\n%s", out)
	}
}

// TestRunE2EExecutesAndWritesReport drives the full `test run e2e`
// command against a mock server: generation, hurl execution with the
// baseUrl variable pointed at the mock, and HTML report emission.
func TestRunE2EExecutesAndWritesReport(t *testing.T) {
	if _, err := exec.LookPath("hurl"); err != nil {
		t.Skip("hurl binary not in PATH; install with `brew install hurl`")
	}

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/widgets":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]string{{"id": "w-42"}},
			})
		case r.Method == "GET" && r.URL.Path == "/widgets/w-42":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "w-42", "name": "Bolt"})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer mock.Close()

	dir := t.TempDir()
	// The OpenAPI servers URL is intentionally a placeholder the mock
	// does not serve: the run must hit --base-url, not this default.
	openapi := `openapi: "3.1.0"
info: { title: Widgets, version: "1.0.0" }
servers:
  - url: https://unused.example.com
paths:
  /widgets:
    get:
      operationId: listWidgets
      responses: { "200": { description: ok } }
  /widgets/{id}:
    get:
      operationId: getWidget
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string }
      responses: { "200": { description: ok } }
`
	writeFile(t, filepath.Join(dir, "openapi.yaml"), openapi)

	arazzo := `arazzo: "1.0.1"
info: { title: t, summary: chain, version: "1.0.0" }
sourceDescriptions:
  - name: api
    url: ./openapi.yaml
    type: openapi
workflows:
  - workflowId: chain
    summary: list then fetch one
    steps:
      - stepId: list
        operationId: listWidgets
        outputs:
          firstId: $response.body#/items/0/id
        successCriteria:
          - condition: $statusCode == 200
      - stepId: get
        operationId: getWidget
        parameters:
          - name: id
            in: path
            value: $steps.list.outputs.firstId
        successCriteria:
          - condition: $statusCode == 200
`
	arazzoPath := filepath.Join(dir, "wf.arazzo.yaml")
	writeFile(t, arazzoPath, arazzo)

	reportDir := filepath.Join(dir, "report")
	stdout, stderr, err := runCmd(t, "test", "run", "e2e", arazzoPath,
		"--base-url", mock.URL, "--report-html", reportDir)
	if err != nil {
		t.Fatalf("test run e2e failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(reportDir, "index.html")); err != nil {
		t.Errorf("expected HTML report index at %s/index.html: %v", reportDir, err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
