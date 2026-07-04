// Package renderer turns model structs into standalone HTML pages.
//
// All entry points take a *theme.Theme. Passing nil falls back to the
// built-in default theme, useful for tests and direct programmatic
// use.
package renderer

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
	"github.com/emmanuelperu/arazzo-maestro/internal/theme"
)

//go:embed templates/*.html
var templatesFS embed.FS

// embeddedExprRe matches the spec's embedded form: a runtime expression
// wrapped in {} curly braces inside a string value.
var embeddedExprRe = regexp.MustCompile(`\{\$[^{}]+\}`)

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
		// html.EscapeString leaves braces and '$' intact, so the
		// embedded {$expr} form can be highlighted on the escaped text.
		h := embeddedExprRe.ReplaceAllStringFunc(html.EscapeString(t), func(m string) string {
			return `<span class="runtime">` + m + `</span>`
		})
		return template.HTML(fmt.Sprintf(`<code class="font-mono text-primary">"%s"</code>`, h))
	case bool:
		return template.HTML(fmt.Sprintf(`<code class="font-mono text-primary">%t</code>`, t))
	case int, int64, int32, float32, float64:
		return template.HTML(fmt.Sprintf(`<code class="font-mono text-primary">%v</code>`, formatNumber(t)))
	default:
		raw, err := json.Marshal(v)
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

// renderPayload pretty-prints a request body payload as JSON,
// highlighting whole-string runtime expressions and the spec's embedded
// {$expr} form. A bare expression inside surrounding text is literal
// data and stays unhighlighted, matching what the test generators
// substitute.
func renderPayload(v any) template.HTML {
	if v == nil {
		return `<span class="text-muted italic">empty</span>`
	}
	probe, err := json.Marshal(v)
	if err != nil {
		return template.HTML(html.EscapeString(fmt.Sprint(v)))
	}
	// A sentinel base absent from the payload guarantees a literal can
	// never be mistaken for a highlight marker.
	base := "__arazzo_hl_"
	for strings.Contains(string(probe), base) {
		base = "_" + base
	}
	var repls []string
	swapped := swapHighlights(v, base, &repls)
	raw, err := json.MarshalIndent(swapped, "", "  ")
	if err != nil {
		return template.HTML(html.EscapeString(fmt.Sprint(v)))
	}
	out := html.EscapeString(string(raw))
	for i, r := range repls {
		out = strings.Replace(out, fmt.Sprintf("%s%d__", base, i), r, 1)
	}
	return template.HTML(out)
}

// swapHighlights walks the payload and replaces highlightable
// expressions with sentinels, recording the trusted HTML to inject
// after marshalling and escaping.
func swapHighlights(v any, base string, repls *[]string) any {
	mark := func(text string) string {
		*repls = append(*repls, `<span class="runtime">`+html.EscapeString(text)+`</span>`)
		return fmt.Sprintf("%s%d__", base, len(*repls)-1)
	}
	switch t := v.(type) {
	case string:
		if isRuntimeExpression(t) {
			return mark(t)
		}
		return embeddedExprRe.ReplaceAllStringFunc(t, mark)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = swapHighlights(val, base, repls)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = swapHighlights(val, base, repls)
		}
		return out
	default:
		return v
	}
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

// Layout selects the workflow diagram orientation. The zero value is
// portrait, the default vertical-rail layout that predates this option.
type Layout string

const (
	LayoutPortrait  Layout = "portrait"
	LayoutLandscape Layout = "landscape"
)

type workflowView struct {
	Workflow  model.Workflow
	Theme     *theme.Theme
	Landscape bool
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
		// opPathTarget decodes an operationPath reference into a short
		// "METHOD /path" label, or "" when the reference is not the
		// canonical spec form (the raw string is shown instead).
		"opPathTarget": func(ref string) string {
			if method, path, ok := oasresolver.OperationPathTarget(ref); ok {
				return method + " " + path
			}
			return ""
		},
		// isLocalWorkflowRef reports whether a step workflowId names a
		// workflow of this document (and thus a sibling HTML page), as
		// opposed to the $sourceDescriptions.<name>.<workflowId> form.
		"isLocalWorkflowRef": func(id string) bool { return !strings.HasPrefix(id, "$") },
		// stepRetrySelfAction returns the first onFailure retry action
		// targeting the step itself (an omitted stepId retries the
		// current step per the Arazzo spec), or nil.
		"stepRetrySelfAction": func(step model.Step) *model.FailureAction {
			for i, a := range step.OnFailure {
				if a.Type == "retry" && (a.StepID == "" || a.StepID == step.StepID) {
					return &step.OnFailure[i]
				}
			}
			return nil
		},
	}
	return template.New("").Funcs(funcs).ParseFS(templatesFS, "templates/*.html")
}

// parsedTemplates parses the embedded templates once; html/template is safe
// for concurrent execution once parsed.
var parsedTemplates = sync.OnceValues(buildTemplate)

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
// A nil theme falls back to the built-in default. The layout selects the
// diagram orientation; an empty value renders portrait.
func RenderWorkflow(wf model.Workflow, th *theme.Theme, layout Layout) (string, error) {
	th, err := resolveTheme(th)
	if err != nil {
		return "", err
	}
	tpl, err := parsedTemplates()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	view := workflowView{Workflow: wf, Theme: th, Landscape: layout == LayoutLandscape}
	if err := tpl.ExecuteTemplate(&buf, "workflow.html", view); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// WriteWorkflow renders a workflow and writes it to
// `<outputDir>/<workflowId>.html`, creating the directory if needed.
func WriteWorkflow(wf model.Workflow, outputDir string, th *theme.Theme, layout Layout) (string, error) {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return "", err
	}
	content, err := RenderWorkflow(wf, th, layout)
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
	tpl, err := parsedTemplates()
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
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
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
