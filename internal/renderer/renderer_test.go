package renderer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/parser"
	"github.com/emmanuelperu/arazzo-maestro/internal/theme"
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
			h, err := RenderWorkflow(w, loadTheme(t, "light"), LayoutPortrait)
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

func TestRenderPayloadHighlightsAlignWithGenerators(t *testing.T) {
	// Whole-string expressions and the spec's braced {$expr} form are
	// highlighted; a bare expression inside surrounding text is data
	// and must NOT be highlighted, since the test generators ship it
	// verbatim.
	html := string(renderPayload(map[string]any{
		"whole":    "$inputs.productId",
		"embedded": "Total: {$inputs.total}",
		"bare":     "Bearer $inputs.token",
	}))
	if !strings.Contains(html, `<span class="runtime">$inputs.productId</span>`) {
		t.Errorf("whole-string expression not highlighted:\n%s", html)
	}
	if !strings.Contains(html, `<span class="runtime">{$inputs.total}</span>`) {
		t.Errorf("braced embedded expression not highlighted:\n%s", html)
	}
	if strings.Contains(html, `<span class="runtime">$inputs.token</span>`) {
		t.Errorf("bare embedded expression must not be highlighted:\n%s", html)
	}
}

func TestRenderValueHighlightsBracedExpression(t *testing.T) {
	html := string(renderValue("Bearer {$inputs.token}"))
	if !strings.Contains(html, `<span class="runtime">{$inputs.token}</span>`) {
		t.Errorf("braced expression not highlighted in value: %s", html)
	}
}

func TestRenderWorkflowShowsRetryLoopWhenStepIDOmitted(t *testing.T) {
	// Per the Arazzo spec a retry action without stepId retries the
	// current step, so the loop curve must render exactly as if the
	// step had targeted itself explicitly.
	w := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "pay",
			OperationID: "payOrder",
			OnFailure: []model.FailureAction{{
				Name:       "retry-pay",
				Type:       "retry",
				RetryAfter: 1.5,
			}},
		}},
	}
	html, err := RenderWorkflow(w, loadTheme(t, "light"), LayoutPortrait)
	if err != nil {
		t.Fatalf("RenderWorkflow: %v", err)
	}
	// An unspecified retryLimit means a single retry per the spec, and
	// retryAfter is a decimal number of seconds.
	for _, want := range []string{`class="step-loop"`, `class="step-loop-curve"`, "× 1", "after 1.5s"} {
		if !strings.Contains(html, want) {
			t.Errorf("output missing %q", want)
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
			html, err = RenderWorkflow(w, loadTheme(t, "light"), LayoutPortrait)
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
		"after 2s",                 // retry after, spec unit: seconds
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
	html, err := RenderWorkflow(doc.Workflows[0], loadTheme(t, "light"), LayoutPortrait)
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
	html, err := RenderWorkflow(doc.Workflows[0], dark, LayoutPortrait)
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
	html, err := RenderWorkflow(doc.Workflows[0], nil, LayoutPortrait)
	if err != nil {
		t.Fatalf("RenderWorkflow(nil): %v", err)
	}
	if !strings.Contains(html, "--bg:") {
		t.Errorf("fallback theme did not produce CSS vars")
	}
}

func TestRenderWorkflowLandscapeTogglesLayoutClass(t *testing.T) {
	doc, err := parser.ParseFile(examplesShopYAML(t))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	wf := doc.Workflows[0]

	portrait, err := RenderWorkflow(wf, loadTheme(t, "light"), LayoutPortrait)
	if err != nil {
		t.Fatalf("RenderWorkflow(portrait): %v", err)
	}
	// The landscape CSS rules ship in both modes; only the body class
	// is conditional, so assert on the class token, not the substring.
	if strings.Contains(portrait, "min-h-screen layout-landscape") {
		t.Errorf("portrait output must not carry the landscape body class")
	}

	landscape, err := RenderWorkflow(wf, loadTheme(t, "light"), LayoutLandscape)
	if err != nil {
		t.Fatalf("RenderWorkflow(landscape): %v", err)
	}
	if !strings.Contains(landscape, `class="min-h-screen layout-landscape"`) {
		t.Errorf("landscape output must add the layout-landscape body class")
	}
}

func TestWriteWorkflowWritesFile(t *testing.T) {
	doc, err := parser.ParseFile(examplesShopYAML(t))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	tmp := t.TempDir()
	path, err := WriteWorkflow(doc.Workflows[0], tmp, loadTheme(t, "light"), LayoutPortrait)
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

func TestRenderWorkflowShowsRequestBodyReplacements(t *testing.T) {
	w := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "create",
			OperationID: "createThing",
			RequestBody: &model.RequestBody{
				ContentType:  "application/json",
				Payload:      map[string]any{"name": "original"},
				Replacements: []model.Replacement{{Target: "/name", Value: "$inputs.token"}},
			},
		}},
	}
	html, err := RenderWorkflow(w, loadTheme(t, "light"), LayoutPortrait)
	if err != nil {
		t.Fatalf("RenderWorkflow: %v", err)
	}
	for _, want := range []string{"Replacements", "/name", `class="runtime"`, "$inputs.token"} {
		if !strings.Contains(html, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRenderWorkflowShowsOperationPathTarget(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "by-path", OperationPath: "{$sourceDescriptions.shop.url}#/paths/~1pet~1findByStatus/get"},
			{StepID: "opaque", OperationPath: "not-the-spec-form"},
		},
	}
	html, err := RenderWorkflow(wf, nil, LayoutPortrait)
	if err != nil {
		t.Fatalf("RenderWorkflow: %v", err)
	}
	// A canonical reference is decoded to method + path; anything else is
	// shown raw so the reader still sees what the document declares.
	if !strings.Contains(html, "GET /pet/findByStatus") {
		t.Error("expected the decoded operationPath target")
	}
	if !strings.Contains(html, "not-the-spec-form") {
		t.Error("expected the raw operationPath fallback")
	}
	if !strings.Contains(html, ">API</span>") {
		t.Error("operationPath steps keep the API tag")
	}
}

func TestRenderWorkflowTagsWorkflowSteps(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "local", WorkflowID: "checkout"},
			{StepID: "external", WorkflowID: "$sourceDescriptions.other.cleanup"},
		},
	}
	html, err := RenderWorkflow(wf, nil, LayoutPortrait)
	if err != nil {
		t.Fatalf("RenderWorkflow: %v", err)
	}
	if !strings.Contains(html, ">workflow</span>") {
		t.Error("expected the workflow step tag instead of API")
	}
	// A local reference links to the sibling page; the qualified form has
	// no page to link to and stays plain text.
	if !strings.Contains(html, `href="checkout.html"`) {
		t.Error("expected a link to the local workflow page")
	}
	if strings.Contains(html, `href="$sourceDescriptions.other.cleanup.html"`) {
		t.Error("qualified workflow reference must not become a broken link")
	}
	if !strings.Contains(html, "$sourceDescriptions.other.cleanup") {
		t.Error("expected the qualified reference to be displayed")
	}
}
