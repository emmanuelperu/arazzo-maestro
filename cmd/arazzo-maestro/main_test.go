// CLI integration tests. They drive the Cobra commands the same way
// the binary does, but capture stdout/stderr into buffers to assert on
// the output. No subprocess spawning, keeps tests fast and stable.

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCmd executes the root command with the given args and returns
// stdout, stderr, and the error returned by Execute.
func runCmd(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := newRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errOut.String(), err
}

func examplesDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestLintShopExampleCleansUp(t *testing.T) {
	path := filepath.Join(examplesDir(t), "shop.arazzo.yaml")
	out, _, err := runCmd(t, "lint", path)
	if err != nil {
		t.Fatalf("lint should pass on shop.arazzo.yaml, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "no issues found") {
		t.Errorf("expected success message, got %q", out)
	}
}

func TestLintExitsNonZeroOnError(t *testing.T) {
	tmp := t.TempDir()
	bad := filepath.Join(tmp, "broken.yaml")
	if err := os.WriteFile(bad, []byte(`arazzo: "2.0.0"`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCmd(t, "lint", bad)
	if err == nil {
		t.Fatal("expected non-zero exit on broken file")
	}
}

func TestLintReportsMissingFile(t *testing.T) {
	_, _, err := runCmd(t, "lint", "/no/such/file.yaml")
	if err == nil {
		t.Fatal("expected an error for missing input file")
	}
	if !strings.Contains(err.Error(), "failed to read") {
		t.Errorf("expected read-error context, got %v", err)
	}
}

func TestViewRendersWorkflows(t *testing.T) {
	tmp := t.TempDir()
	out, _, err := runCmd(t, "view", filepath.Join(examplesDir(t), "shop.arazzo.yaml"), "-o", tmp)
	if err != nil {
		t.Fatalf("view failed: %v\n%s", err, out)
	}
	for _, name := range []string{"happy-path-checkout.html", "payment-refused-path.html", "index.html"} {
		if _, statErr := os.Stat(filepath.Join(tmp, name)); statErr != nil {
			t.Errorf("expected %s to be generated: %v", name, statErr)
		}
	}
}

func TestViewWithDarkTheme(t *testing.T) {
	tmp := t.TempDir()
	if _, _, err := runCmd(t, "view", filepath.Join(examplesDir(t), "shop.arazzo.yaml"), "-o", tmp, "--theme", "dark"); err != nil {
		t.Fatalf("view --theme dark failed: %v", err)
	}
	html, err := os.ReadFile(filepath.Join(tmp, "happy-path-checkout.html"))
	if err != nil {
		t.Fatal(err)
	}
	// Dark theme `bg` colour from builtin.yml (Blueprint dark palette).
	if !strings.Contains(string(html), "--bg: #0a0a0f") {
		t.Errorf("expected dark theme background in output")
	}
}

func TestViewWithUnknownThemeFails(t *testing.T) {
	_, _, err := runCmd(t, "view", filepath.Join(examplesDir(t), "shop.arazzo.yaml"), "--theme", "ghost", "-o", t.TempDir())
	if err == nil {
		t.Fatal("expected error for unknown theme")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %v", err)
	}
}

func TestViewWithWorkflowFilter(t *testing.T) {
	tmp := t.TempDir()
	if _, _, err := runCmd(t, "view", filepath.Join(examplesDir(t), "shop.arazzo.yaml"), "-o", tmp, "--workflow", "happy-path-checkout"); err != nil {
		t.Fatalf("view --workflow failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "happy-path-checkout.html")); err != nil {
		t.Errorf("expected the selected workflow to be generated: %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "payment-refused-path.html")); !os.IsNotExist(err) {
		t.Errorf("expected the non-selected workflow to be absent, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "index.html")); !os.IsNotExist(err) {
		t.Errorf("expected no index when --workflow is set, stat err = %v", err)
	}
}

func TestViewUnknownWorkflowFails(t *testing.T) {
	_, _, err := runCmd(t, "view", filepath.Join(examplesDir(t), "shop.arazzo.yaml"), "--workflow", "ghost", "-o", t.TempDir())
	if err == nil {
		t.Fatal("expected error for unknown workflow")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %v", err)
	}
}

func TestListThemes(t *testing.T) {
	out, _, err := runCmd(t, "view", "--list-themes")
	if err != nil {
		t.Fatalf("--list-themes failed: %v", err)
	}
	if !strings.Contains(out, "light (default)") {
		t.Errorf("expected 'light (default)' in output, got %q", out)
	}
	if !strings.Contains(out, "dark") {
		t.Errorf("expected 'dark' in output, got %q", out)
	}
}

func TestVersionStringPrefersInjectedValue(t *testing.T) {
	old := version
	defer func() { version = old }()

	version = "0.3.0"
	if got := versionString(); got != "0.3.0" {
		t.Errorf("versionString() = %q, want the injected 0.3.0", got)
	}

	// In `go test` the build info reports "(devel)", so the dev
	// default must survive the fallback.
	version = "dev"
	if got := versionString(); got != "dev" {
		t.Errorf("versionString() = %q, want dev for source builds", got)
	}
}
