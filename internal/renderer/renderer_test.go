package renderer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arazzo-maestro/internal/parser"
	"arazzo-maestro/internal/theme"
)

func examplesShopYAML(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "examples", "shop.arazzo.yaml")
}

func loadTheme(t *testing.T, name string) *theme.Theme {
	t.Helper()
	r, err := theme.LoadBuiltin()
	if err != nil {
		t.Fatalf("LoadBuiltin: %v", err)
	}
	th, err := r.Resolve(name)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", name, err)
	}
	return th
}

func TestRenderValueHighlightsRuntimeExpression(t *testing.T) {
	out := string(renderValue("$inputs.productId"))
	if !strings.Contains(out, `class="runtime"`) {
		t.Errorf("missing runtime class in %q", out)
	}
	if !strings.Contains(out, "$inputs.productId") {
		t.Errorf("missing expression in %q", out)
	}
}

func TestRenderValueQuotesPlainString(t *testing.T) {
	out := string(renderValue("fr_FR"))
	if !strings.Contains(out, `"fr_FR"`) {
		t.Errorf("missing quoted string in %q", out)
	}
	if strings.Contains(out, "runtime") {
		t.Errorf("unexpected runtime class in %q", out)
	}
}

func TestRenderValueHandlesNumbersAndNil(t *testing.T) {
	if !strings.Contains(string(renderValue(int64(10))), "10") {
		t.Error("renderValue(10) missing 10")
	}
	if !strings.Contains(string(renderValue(true)), "true") {
		t.Error("renderValue(true) missing true")
	}
	if !strings.Contains(string(renderValue(nil)), "null") {
		t.Error("renderValue(nil) missing null")
	}
}

func TestRenderValueHandlesEveryScalarKind(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"int", 42, "42"},
		{"int32", int32(42), "42"},
		{"int64", int64(42), "42"},
		{"float32", float32(3.14), "3.14"},
		{"float64", 2.5, "2.5"},
		{"false", false, "false"},
		{"empty string", "", `""`},
		{"list", []any{1, 2}, "[1,2]"},
		{"map", map[string]any{"k": "v"}, "k"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := string(renderValue(c.v))
			if !strings.Contains(out, c.want) {
				t.Errorf("renderValue(%v) = %q, want substring %q", c.v, out, c.want)
			}
		})
	}
}

func TestRenderDefault(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"plain", "plain"},
		{true, "true"},
		{int64(7), "7"},
		{3.14, "3.14"},
	}
	for _, c := range cases {
		if got := renderDefault(c.in); got != c.want {
			t.Errorf("renderDefault(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderPayloadNil(t *testing.T) {
	out := string(renderPayload(nil))
	if !strings.Contains(out, "empty") {
		t.Errorf("renderPayload(nil) should mention 'empty', got %q", out)
	}
}

func TestRenderPayloadHighlightsExpressionsInsideJSON(t *testing.T) {
	payload := map[string]any{
		"productId": "$inputs.productId",
		"quantity":  int64(2),
	}
	out := string(renderPayload(payload))
	if !strings.Contains(out, `class="runtime"`) {
		t.Errorf("missing runtime class in %q", out)
	}
	if !strings.Contains(out, "$inputs.productId") {
		t.Errorf("missing expression in %q", out)
	}
}

func TestRenderWorkflowContainsExpectedSections(t *testing.T) {
	doc, err := parser.ParseFile(examplesShopYAML(t))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	var html string
	for _, w := range doc.Workflows {
		if w.WorkflowID == "happy-path-checkout" {
			h, err := RenderWorkflow(w, loadTheme(t, "light"))
			if err != nil {
				t.Fatalf("RenderWorkflow: %v", err)
			}
			html = h
			break
		}
	}
	if html == "" {
		t.Fatal("happy-path-checkout workflow not found in example doc")
	}

	for _, want := range []string{
		"<!doctype html>",
		"cdn.tailwindcss.com",
		"happy-path-checkout",
		"list-catalog",
		"add-to-cart",
		"pay",
		"productId",
		"transactionId",
		`class="runtime"`,
		"$inputs.productId",
		// CSS variables and semantic markup are part of the contract:
		"--bg:",
		"--runtime:",
		`<h1`,
		`<h2`,
		`<h3`,
		`aria-hidden="true"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

func TestRenderWorkflowShowsRetryAction(t *testing.T) {
	// The enriched payment-refused-path workflow declares an onFailure
	// retry action on the pay-refused step, the renderer must surface
	// it visibly.
	doc, err := parser.ParseFile(examplesShopYAML(t))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	var html string
	for _, w := range doc.Workflows {
		if w.WorkflowID == "payment-refused-path" {
			html, err = RenderWorkflow(w, loadTheme(t, "light"))
			if err != nil {
				t.Fatalf("RenderWorkflow: %v", err)
			}
			break
		}
	}
	if html == "" {
		t.Fatal("payment-refused-path workflow not found")
	}
	for _, want := range []string{
		"On Failure",
		"retry",                    // action-tag
		"× 2",                      // retry limit
		"2000ms",                   // retry after
		"$statusCode &gt;= 500",    // criterion, HTML-escaped
		"end",                      // second action
		`class="step-loop"`,        // visible loop curve container
		`class="step-loop-curve"`,  // the CSS-drawn arc
		`class="step-loop-label"`,  // annotation next to the curve
		`href="#step-pay-refused"`, // anchor link to the target step
	} {
		if !strings.Contains(html, want) {
			t.Errorf("expected output to contain %q", want)
		}
	}
}

// Regression: html/template's CSS-context auto-escape used to emit
// `ZgotmplZ` when interpolating font-family stacks containing
// double-quoted names like "Segoe UI". The fix wraps fontStack's
// return as template.CSS; this test guards against the regression.
func TestRenderWorkflowFontStackNotEscaped(t *testing.T) {
	doc, _ := parser.ParseFile(examplesShopYAML(t))
	html, err := RenderWorkflow(doc.Workflows[0], loadTheme(t, "light"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "ZgotmplZ") {
		t.Error("rendered HTML contains ZgotmplZ, a CSS-context interpolation was escaped incorrectly")
	}
	if !strings.Contains(html, "ui-sans-serif") {
		t.Error("expected the resolved sans font stack to be present")
	}
}

func TestRenderWorkflowDarkThemeInjectsDarkColors(t *testing.T) {
	doc, err := parser.ParseFile(examplesShopYAML(t))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	dark := loadTheme(t, "dark")
	html, err := RenderWorkflow(doc.Workflows[0], dark)
	if err != nil {
		t.Fatalf("RenderWorkflow: %v", err)
	}
	// Dark bg must show up in the :root block.
	if !strings.Contains(html, "--bg: "+dark.Colors["bg"]) {
		t.Errorf("expected --bg: %s in output", dark.Colors["bg"])
	}
	// No Google Fonts or external font <link> ever (eco-design rule).
	if strings.Contains(html, "fonts.googleapis.com") || strings.Contains(html, "@font-face") {
		t.Errorf("output contains external font reference, violates eco-design rule")
	}
}

func TestRenderWorkflowNilThemeFallsBackToLight(t *testing.T) {
	doc, _ := parser.ParseFile(examplesShopYAML(t))
	html, err := RenderWorkflow(doc.Workflows[0], nil)
	if err != nil {
		t.Fatalf("RenderWorkflow(nil): %v", err)
	}
	if !strings.Contains(html, "--bg:") {
		t.Errorf("fallback theme did not produce CSS vars")
	}
}

func TestWriteWorkflowWritesFile(t *testing.T) {
	doc, err := parser.ParseFile(examplesShopYAML(t))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	tmp := t.TempDir()
	path, err := WriteWorkflow(doc.Workflows[0], tmp, loadTheme(t, "light"))
	if err != nil {
		t.Fatalf("WriteWorkflow: %v", err)
	}
	if filepath.Base(path) != "happy-path-checkout.html" {
		t.Errorf("path = %q, want suffix happy-path-checkout.html", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(content), "<!doctype html>") {
		t.Errorf("file content missing doctype")
	}
}

func TestRenderIndexListsEveryWorkflow(t *testing.T) {
	doc, err := parser.ParseFile(examplesShopYAML(t))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	html, err := RenderIndex(doc, loadTheme(t, "light"))
	if err != nil {
		t.Fatalf("RenderIndex: %v", err)
	}
	for _, want := range []string{
		"happy-path-checkout.html",
		"payment-refused-path.html",
		"Shop workflows",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index missing %q", want)
		}
	}
}

func TestWriteIndexWritesFile(t *testing.T) {
	doc, err := parser.ParseFile(examplesShopYAML(t))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	tmp := t.TempDir()
	path, err := WriteIndex(doc, tmp, loadTheme(t, "light"))
	if err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}
	if filepath.Base(path) != "index.html" {
		t.Errorf("path = %q, want index.html", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("stat: %v", err)
	}
}
