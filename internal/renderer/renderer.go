// Package renderer turns model structs into standalone HTML pages.
//
// All entry points take a *theme.Theme. Passing nil falls back to the
// built-in default theme, useful for tests and direct programmatic
// use.
package renderer

import (
	"bytes"
	"embed"
	"fmt"
	"html"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	jsonenc "encoding/json"

	"arazzo-maestro/internal/model"
	"arazzo-maestro/internal/theme"
)

//go:embed templates/*.html
var templatesFS embed.FS

// runtimeExprRe matches Arazzo runtime expressions like $inputs.X,
// $response.body#/foo, $steps.add-to-cart.outputs.cartTotal, $statusCode.
var runtimeExprRe = regexp.MustCompile(`\$[A-Za-z][A-Za-z0-9_]*(?:[.#/\-][A-Za-z0-9_\-]+)*`)

func isRuntimeExpression(v any) bool {
	s, ok := v.(string)
	return ok && strings.HasPrefix(s, "$")
}

// renderValue renders a scalar value with Arazzo runtime expressions
// highlighted. Output is trusted HTML and must therefore be wrapped in
// template.HTML so html/template does not re-escape it.
func renderValue(v any) template.HTML {
	if v == nil {
		return `<span class="text-muted italic">null</span>`
	}
	switch t := v.(type) {
	case string:
		if isRuntimeExpression(t) {
			return template.HTML(fmt.Sprintf(`<code class="runtime">%s</code>`, html.EscapeString(t)))
		}
		return template.HTML(fmt.Sprintf(`<code class="font-mono text-primary">"%s"</code>`, html.EscapeString(t)))
	case bool:
		return template.HTML(fmt.Sprintf(`<code class="font-mono text-primary">%t</code>`, t))
	case int, int64, int32, float32, float64:
		return template.HTML(fmt.Sprintf(`<code class="font-mono text-primary">%v</code>`, formatNumber(t)))
	default:
		raw, err := jsonenc.Marshal(v)
		if err != nil {
			return template.HTML(fmt.Sprintf(`<code class="font-mono text-primary">%s</code>`, html.EscapeString(fmt.Sprint(v))))
		}
		return template.HTML(fmt.Sprintf(`<code class="font-mono text-primary">%s</code>`, html.EscapeString(string(raw))))
	}
}

func formatNumber(v any) string {
	switch n := v.(type) {
	case int:
		return fmt.Sprintf("%d", n)
	case int32:
		return fmt.Sprintf("%d", n)
	case int64:
		return fmt.Sprintf("%d", n)
	case float32:
		return fmt.Sprintf("%g", n)
	case float64:
		return fmt.Sprintf("%g", n)
	}
	return fmt.Sprint(v)
}

// renderPayload pretty-prints a request body payload as JSON, highlighting
// any runtime expressions found inside string values.
func renderPayload(v any) template.HTML {
	if v == nil {
		return `<span class="text-muted italic">empty</span>`
	}
	raw, err := jsonenc.MarshalIndent(v, "", "  ")
	if err != nil {
		return template.HTML(html.EscapeString(fmt.Sprint(v)))
	}
	escaped := html.EscapeString(string(raw))
	highlighted := runtimeExprRe.ReplaceAllStringFunc(escaped, func(m string) string {
		return `<span class="runtime">` + m + `</span>`
	})
	return template.HTML(highlighted)
}

// renderDefault prints a default value as a bare scalar.
func renderDefault(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return fmt.Sprintf("%t", t)
	}
	return formatNumber(v)
}

type workflowView struct {
	Workflow model.Workflow
	Theme    *theme.Theme
}

type indexView struct {
	Document *model.ArazzoDocument
	Theme    *theme.Theme
}

func buildTemplate() (*template.Template, error) {
	funcs := template.FuncMap{
		"renderValue":   renderValue,
		"renderPayload": renderPayload,
		"renderDefault": renderDefault,
		"trim":          strings.TrimSpace,
		"hasDefault":    func(v any) bool { return v != nil },
		"requestContentType": func(b *model.RequestBody) string {
			if b == nil || b.ContentType == "" {
				return "application/json"
			}
			return b.ContentType
		},
		"pluralS": func(n int) string {
			if n == 1 {
				return ""
			}
			return "s"
		},
		"defaultTitle": func(title, fallback string) string {
			if title != "" {
				return title
			}
			return fallback
		},
		// fontStack returns a hardcoded system font stack; wrapping in
		// template.CSS bypasses html/template's CSS-context escaping,
		// which would otherwise emit `ZgotmplZ` because the stack
		// contains double-quoted family names like "Segoe UI".
		"fontStack": func(name string) template.CSS {
			return template.CSS(theme.FontStack(name))
		},
		"add1": func(i int) int { return i + 1 },
		// stepRetrySelfAction returns the first onFailure retry action
		// targeting the step itself, or nil. Used to draw the visible
		// loop curve on the right side of the step with the retry
		// meta inline.
		"stepRetrySelfAction": func(step model.Step) *model.FailureAction {
			for i, a := range step.OnFailure {
				if a.Type == "retry" && a.StepID == step.StepID {
					return &step.OnFailure[i]
				}
			}
			return nil
		},
	}
	return template.New("").Funcs(funcs).ParseFS(templatesFS, "templates/*.html")
}

// fallbackTheme is used when a caller passes a nil theme. It mirrors
// the built-in light theme so tests and ad-hoc rendering keep working.
func fallbackTheme() (*theme.Theme, error) {
	r, err := theme.LoadBuiltin()
	if err != nil {
		return nil, err
	}
	return r.Resolve("")
}

func resolveTheme(t *theme.Theme) (*theme.Theme, error) {
	if t != nil {
		return t, nil
	}
	return fallbackTheme()
}

// RenderWorkflow renders a single workflow to a standalone HTML string.
// A nil theme falls back to the built-in default.
func RenderWorkflow(wf model.Workflow, th *theme.Theme) (string, error) {
	th, err := resolveTheme(th)
	if err != nil {
		return "", err
	}
	tpl, err := buildTemplate()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "workflow.html", workflowView{Workflow: wf, Theme: th}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// WriteWorkflow renders a workflow and writes it to
// `<outputDir>/<workflowId>.html`, creating the directory if needed.
func WriteWorkflow(wf model.Workflow, outputDir string, th *theme.Theme) (string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	content, err := RenderWorkflow(wf, th)
	if err != nil {
		return "", err
	}
	path := filepath.Join(outputDir, wf.WorkflowID+".html")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// RenderIndex renders the index page linking to every workflow.
func RenderIndex(doc *model.ArazzoDocument, th *theme.Theme) (string, error) {
	th, err := resolveTheme(th)
	if err != nil {
		return "", err
	}
	tpl, err := buildTemplate()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.ExecuteTemplate(&buf, "index.html", indexView{Document: doc, Theme: th}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// WriteIndex renders and writes the index page to `<outputDir>/index.html`.
func WriteIndex(doc *model.ArazzoDocument, outputDir string, th *theme.Theme) (string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	content, err := RenderIndex(doc, th)
	if err != nil {
		return "", err
	}
	path := filepath.Join(outputDir, "index.html")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
