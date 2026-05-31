package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenE2EAgainstShopExample(t *testing.T) {
	out := t.TempDir()
	stdout, _, err := runCmd(t, "test", "gen", "e2e",
		filepath.Join(examplesDir(t), "shop.arazzo.yaml"),
		"-o", out)
	if err != nil {
		t.Fatalf("test gen e2e failed: %v\n%s", err, stdout)
	}
	shopDir := filepath.Join(out, "e2e", "hurl", "shop")
	for _, name := range []string{"happy-path-checkout.hurl", "payment-refused-path.hurl"} {
		path := filepath.Join(shopDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s to exist: %v", path, err)
		}
		if !strings.Contains(stdout, name) {
			t.Errorf("stdout should mention written file %q, got %q", name, stdout)
		}
	}
	body, err := os.ReadFile(filepath.Join(shopDir, "happy-path-checkout.hurl"))
	if err != nil {
		t.Fatalf("read generated hurl: %v", err)
	}
	got := string(body)
	for _, want := range []string{
		"# Workflow: happy-path-checkout",
		"GET {{baseUrl}}/products",
		"# Base URL (required)",
		"[QueryStringParams]",
		"[Captures]",
		`jsonpath "`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated hurl missing %q\n--- output ---\n%s", want, got)
		}
	}
	if strings.Contains(got, "__unresolved__") {
		t.Errorf("operation resolution failed on a valid example:\n%s", got)
	}
}

func TestGenE2EFiltersByWorkflowID(t *testing.T) {
	out := t.TempDir()
	stdout, _, err := runCmd(t, "test", "gen", "e2e",
		filepath.Join(examplesDir(t), "shop.arazzo.yaml"),
		"-o", out, "--workflow", "happy-path-checkout")
	if err != nil {
		t.Fatalf("filtered run failed: %v\n%s", err, stdout)
	}
	shopDir := filepath.Join(out, "e2e", "hurl", "shop")
	if _, err := os.Stat(filepath.Join(shopDir, "happy-path-checkout.hurl")); err != nil {
		t.Errorf("happy-path-checkout.hurl should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(shopDir, "payment-refused-path.hurl")); !os.IsNotExist(err) {
		t.Errorf("payment-refused-path.hurl should NOT exist when filtering, stat err = %v", err)
	}
}

func TestGenE2EDerivesArazzoBaseName(t *testing.T) {
	// Files named 'foo.arazzo.yaml' should land under
	// '<out>/e2e/<format>/foo/...', i.e. the '.arazzo.yaml' suffix
	// is stripped (not just the last extension).
	out := t.TempDir()
	_, _, err := runCmd(t, "test", "gen", "e2e",
		filepath.Join(examplesDir(t), "checkout-branching.arazzo.yaml"),
		"-o", out)
	if err != nil {
		t.Fatalf("test gen e2e failed: %v", err)
	}
	want := filepath.Join(out, "e2e", "hurl", "checkout-branching", "checkout-with-branching.hurl")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected %s, stat err = %v", want, err)
	}
}

func TestGenE2ERejectsUnknownWorkflow(t *testing.T) {
	_, _, err := runCmd(t, "test", "gen", "e2e",
		filepath.Join(examplesDir(t), "shop.arazzo.yaml"),
		"-o", t.TempDir(), "--workflow", "nope")
	if err == nil {
		t.Fatal("expected an error for unknown workflow id")
	}
	if !strings.Contains(err.Error(), `"nope" not found`) {
		t.Errorf("error should explain the missing workflow, got %v", err)
	}
}

func TestGenE2ERejectsUnsupportedFormat(t *testing.T) {
	_, _, err := runCmd(t, "test", "gen", "e2e",
		filepath.Join(examplesDir(t), "shop.arazzo.yaml"),
		"-o", t.TempDir(), "--format", "k6")
	if err == nil {
		t.Fatal("expected an error for unsupported format on e2e")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("error should mention unsupported format, got %v", err)
	}
}

func TestGenE2EFailsOnMissingArazzoFile(t *testing.T) {
	_, _, err := runCmd(t, "test", "gen", "e2e", "/no/such/file.yaml", "-o", t.TempDir())
	if err == nil {
		t.Fatal("expected an error for missing arazzo input")
	}
}

func TestGenE2EFailsOnMissingSourceFile(t *testing.T) {
	dir := t.TempDir()
	arazzo := filepath.Join(dir, "wf.arazzo.yaml")
	body := `arazzo: "1.0.1"
info: { title: t, version: "1.0.0", summary: t }
sourceDescriptions:
  - name: api
    url: ./not-here.yaml
    type: openapi
workflows:
  - workflowId: w
    summary: w
    steps:
      - stepId: s
        operationId: ping
`
	if err := os.WriteFile(arazzo, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCmd(t, "test", "gen", "e2e", arazzo, "-o", t.TempDir())
	if err == nil {
		t.Fatal("expected an error when sourceDescription points at a missing file")
	}
	if !strings.Contains(err.Error(), "file not found") {
		t.Errorf("error should mention file not found, got %v", err)
	}
}

func TestGenE2ERejectsHTTPSSource(t *testing.T) {
	dir := t.TempDir()
	arazzo := filepath.Join(dir, "wf.arazzo.yaml")
	body := `arazzo: "1.0.1"
info: { title: t, version: "1.0.0", summary: t }
sourceDescriptions:
  - name: api
    url: https://example.com/openapi.yaml
    type: openapi
workflows:
  - workflowId: w
    summary: w
    steps:
      - stepId: s
        operationId: ping
`
	if err := os.WriteFile(arazzo, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCmd(t, "test", "gen", "e2e", arazzo, "-o", t.TempDir())
	if err == nil {
		t.Fatal("expected an error for HTTPS sourceDescription URL")
	}
	if !strings.Contains(err.Error(), "HTTP source URLs are not supported") {
		t.Errorf("error should mention HTTP source rejection, got %v", err)
	}
}

func TestRunE2ERequiresBaseURL(t *testing.T) {
	_, _, err := runCmd(t, "test", "run", "e2e",
		filepath.Join(examplesDir(t), "shop.arazzo.yaml"))
	if err == nil {
		t.Fatal("expected an error when --base-url is omitted")
	}
	if !strings.Contains(err.Error(), "base-url") {
		t.Errorf("error should name the required base-url flag, got %v", err)
	}
}

func TestRunE2ERejectsNonHTTPBaseURL(t *testing.T) {
	_, _, err := runCmd(t, "test", "run", "e2e",
		filepath.Join(examplesDir(t), "shop.arazzo.yaml"),
		"--base-url", "ftp://nope.example.com")
	if err == nil {
		t.Fatal("expected an error for a non-http(s) base-url")
	}
	if !strings.Contains(err.Error(), "http(s)") {
		t.Errorf("error should explain the http(s) requirement, got %v", err)
	}
}

func TestRunE2ERejectsUnsupportedFormat(t *testing.T) {
	_, _, err := runCmd(t, "test", "run", "e2e",
		filepath.Join(examplesDir(t), "shop.arazzo.yaml"),
		"--base-url", "https://staging.example.com", "--format", "k6")
	if err == nil {
		t.Fatal("expected an error for an unsupported run format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("error should mention unsupported format, got %v", err)
	}
}

func TestRunE2EHelpAdvertisesEndpointAndReport(t *testing.T) {
	stdout, _, err := runCmd(t, "test", "run", "e2e", "--help")
	if err != nil {
		t.Fatalf("test run e2e --help failed: %v", err)
	}
	for _, want := range []string{"--base-url", "--report-html", "--variable", "--workflow"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("'test run e2e --help' should mention %q, got:\n%s", want, stdout)
		}
	}
}

func TestGenE2EHelpAdvertisesFormatsAndPerf(t *testing.T) {
	// The 'test' parent command help must surface the e2e/perf split
	// even though perf is not implemented yet, so users understand the
	// CLI structure they will use when perf lands.
	stdout, _, err := runCmd(t, "test", "--help")
	if err != nil {
		t.Fatalf("test --help failed: %v", err)
	}
	for _, want := range []string{"e2e", "perf", "hurl", "k6"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("'test --help' should mention %q, got:\n%s", want, stdout)
		}
	}

	// The e2e subcommand help must document the format flag and the
	// supported value(s) so users see at a glance how to invoke it.
	stdout, _, err = runCmd(t, "test", "gen", "e2e", "--help")
	if err != nil {
		t.Fatalf("test gen e2e --help failed: %v", err)
	}
	for _, want := range []string{"--format", "hurl", "--workflow", "--output"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("'test gen e2e --help' should mention %q, got:\n%s", want, stdout)
		}
	}
}
