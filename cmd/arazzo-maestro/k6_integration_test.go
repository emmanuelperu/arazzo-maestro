//go:build integration

// Integration test that runs the real `k6` binary against a generated
// perf script. The mock server, OpenAPI source, and Arazzo workflow are
// built on the fly to keep the test self-contained.
//
// Run with: go test -tags=integration ./cmd/arazzo-maestro/
// Requires the k6 binary in PATH (install via `brew install k6`).

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestK6RunsGeneratedScriptAgainstMockServer(t *testing.T) {
	if _, err := exec.LookPath("k6"); err != nil {
		t.Skip("k6 binary not in PATH; install with `brew install k6`")
	}

	// The mock answers /widgets and, only when the captured id is
	// substituted correctly, /widgets/w-42. Any other path means the
	// step chaining broke, which fails the test from inside the handler.
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
			t.Errorf("unexpected request: %s %s (chain broken?)", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer mock.Close()

	dir := t.TempDir()
	// The OpenAPI servers URL is a placeholder the mock does not serve:
	// the run must hit BASE_URL, not this default.
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

	outDir := filepath.Join(dir, "gen")
	// A failing-checks threshold turns any unmet status criterion into a
	// non-zero k6 exit, so a broken chain fails the run, not just a log line.
	stdout, _, err := runCmd(t, "test", "gen", "perf", arazzoPath, "-o", outDir,
		"--threshold", "checks=rate>0.99")
	if err != nil {
		t.Fatalf("test gen perf failed: %v\n%s", err, stdout)
	}

	k6File := filepath.Join(outDir, "perf", "k6", "wf", "chain.k6.js")
	if _, err := os.Stat(k6File); err != nil {
		t.Fatalf("expected generated file at %s: %v", k6File, err)
	}

	out, err := exec.Command("k6", "run", "--quiet", "--no-color",
		"--vus", "1", "--iterations", "1",
		"-e", "BASE_URL="+mock.URL, k6File).CombinedOutput()
	if err != nil {
		body, _ := os.ReadFile(k6File)
		t.Fatalf("`k6 run` failed (checks unmet or chain broken): %v\n--- generated k6 ---\n%s\n--- k6 output ---\n%s",
			err, body, out)
	}
}
