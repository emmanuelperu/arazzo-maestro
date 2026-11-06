package renderer

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/emmanuelperu/arazzo-maestro/internal/parser"
)

func TestRenderedPagesMakeNoRequest(t *testing.T) {
	doc, err := parser.ParseFile(examplesShopYAML(t))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	page, err := RenderWorkflow(doc.Workflows[0], loadTheme(t, "light"), LayoutPortrait)
	if err != nil {
		t.Fatalf("RenderWorkflow: %v", err)
	}
	index, err := RenderIndex(doc, loadTheme(t, "light"))
	if err != nil {
		t.Fatalf("RenderIndex: %v", err)
	}
	for name, html := range map[string]string{"workflow": page, "index": index} {
		for _, banned := range []string{"<script", "cdn.tailwindcss.com", `<link rel="stylesheet"`} {
			if strings.Contains(html, banned) {
				t.Errorf("%s page contains %q", name, banned)
			}
		}
		// Tailwind's preflight reset proves the embedded CSS was inlined.
		if !strings.Contains(html, "box-sizing:border-box") {
			t.Errorf("%s page lacks the embedded Tailwind CSS", name)
		}
	}
}

var (
	classAttrRe   = regexp.MustCompile(`class="([^"]*)"`)
	templateActRe = regexp.MustCompile(`\{\{.*?\}\}`)
	styleBlockRe  = regexp.MustCompile(`(?s)<style>(.*?)</style>`)
)

// TestEmbeddedCSSCoversTemplateClasses fails when a template or renderer.go
// starts using a Tailwind class that templates/tailwind.css does not define:
// run `make css` to regenerate it.
func TestEmbeddedCSSCoversTemplateClasses(t *testing.T) {
	sources := []string{"templates/workflow.html", "templates/index.html", "renderer.go"}
	var ownCSS strings.Builder
	classes := map[string]string{}
	for _, src := range sources {
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read %s: %v", src, err)
		}
		for _, m := range styleBlockRe.FindAllStringSubmatch(string(raw), -1) {
			ownCSS.WriteString(m[1])
		}
		for _, m := range classAttrRe.FindAllStringSubmatch(string(raw), -1) {
			for _, c := range strings.Fields(templateActRe.ReplaceAllString(m[1], " ")) {
				classes[c] = src
			}
		}
	}
	defined := tailwindCSS + ownCSS.String()
	for c, src := range classes {
		selector := "." + strings.NewReplacer(":", `\:`, "/", `\/`, ".", `\.`, "[", `\[`, "]", `\]`).Replace(c)
		if !regexp.MustCompile(regexp.QuoteMeta(selector) + `[\s{,:.>)\[]`).MatchString(defined) {
			t.Errorf("class %q used in %s is defined neither by templates/tailwind.css nor by the template styles", c, src)
		}
	}
}
