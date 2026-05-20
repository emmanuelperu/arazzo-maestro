// Package theme loads, validates and merges visual themes for the
// HTML renderer. Themes are named, indivisible units (palette + font +
// shape).
//
// At startup the binary loads its built-in themes (embedded via
// //go:embed). An optional user `themes.yml` file at the project root
// can add new themes or override built-ins by name, and override the
// default theme via a top-level `default:` field.
//
// See .agents/rules/eco-design.md and .agents/rules/accessibility.md
// for the constraints that shape the validation rules (system fonts
// only, WCAG AA contrast).
package theme

import (
	_ "embed"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"

	"gopkg.in/yaml.v3"
)

// BuiltinDefault is the hard-coded fallback theme name. The built-in
// YAML must define a theme with this name.
const BuiltinDefault = "light"

// validFonts and validShapes are the only accepted values for those
// fields. Custom fonts (Google Fonts, @font-face) are intentionally
// excluded; see .agents/rules/eco-design.md.
var (
	validFonts  = map[string]bool{"sans": true, "serif": true, "mono": true}
	validShapes = map[string]bool{"rounded": true, "square": true}
)

// requiredColors lists the keys every theme must define. Keeping it
// explicit lets us catch typos in user themes early instead of silently
// rendering with missing colors.
//
// The schema matches the "Blueprint" visual identity: flat fills with
// hairline frames instead of gradients, see Plan.md > Visual direction.
var requiredColors = []string{
	"bg", "bgGrid",
	"text", "textMuted", "heading",
	"cardBg", "cardBorder", "cardHeaderBg",
	"runtime", "apiLink",
	"startBg", "startBorder", "startText", "startMuted",
	"endBg", "endBorder", "endText", "endMuted",
	"jsonBg", "jsonText", "jsonRuntime",
	"successBg", "successBorder", "successText",
	"rail",
	"badgeBg", "badgeBorder", "badgeText",
	"footer",
}

// hexColorRe matches #RGB, #RRGGBB, or #RRGGBBAA.
var hexColorRe = regexp.MustCompile(`^#([0-9A-Fa-f]{3}|[0-9A-Fa-f]{6}|[0-9A-Fa-f]{8})$`)

// Theme is a fully-resolved theme ready to be passed to the renderer.
type Theme struct {
	Name   string
	Font   string // "sans" | "serif" | "mono"
	Shape  string // "rounded" | "square"
	Colors map[string]string
}

// Registry holds the merged set of built-in and user themes and the
// resolved default theme name.
type Registry struct {
	themes  map[string]Theme
	Default string
}

//go:embed themes/builtin.yml
var builtinYAML []byte

// rawFile mirrors the YAML structure on disk.
type rawFile struct {
	Default string     `yaml:"default"`
	Themes  []rawTheme `yaml:"themes"`
}

type rawTheme struct {
	Name   string            `yaml:"name"`
	Font   string            `yaml:"font"`
	Shape  string            `yaml:"shape"`
	Colors map[string]string `yaml:"colors"`
}

// LoadBuiltin returns a Registry populated with the embedded built-in
// themes. Its Default field is set to BuiltinDefault.
func LoadBuiltin() (*Registry, error) {
	themes, _, err := parseBytes(builtinYAML, "builtin.yml")
	if err != nil {
		return nil, fmt.Errorf("loading built-in themes: %w", err)
	}
	r := &Registry{themes: make(map[string]Theme, len(themes)), Default: BuiltinDefault}
	for _, t := range themes {
		r.themes[t.Name] = t
	}
	if _, ok := r.themes[BuiltinDefault]; !ok {
		return nil, fmt.Errorf("built-in themes do not define %q", BuiltinDefault)
	}
	return r, nil
}

// MergeFile loads a user themes file and merges it into the registry:
// themes with a matching name replace built-ins, others are added. If
// the file declares a `default:` field, it overrides the registry's
// current default (which must resolve to a known theme).
func (r *Registry) MergeFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	themes, defaultName, err := parseBytes(raw, path)
	if err != nil {
		return err
	}
	for _, t := range themes {
		r.themes[t.Name] = t
	}
	if defaultName != "" {
		r.Default = defaultName
	}
	if _, ok := r.themes[r.Default]; !ok {
		return fmt.Errorf("%s: default theme %q is not defined", path, r.Default)
	}
	return nil
}

// Resolve returns the theme to render with. An empty explicit name
// falls back to the registry's resolved default.
func (r *Registry) Resolve(explicit string) (*Theme, error) {
	name := explicit
	if name == "" {
		name = r.Default
	}
	t, ok := r.themes[name]
	if !ok {
		return nil, fmt.Errorf("theme %q not found. Available: %s", name, joinNames(r.List()))
	}
	return &t, nil
}

// List returns the theme names sorted alphabetically.
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.themes))
	for name := range r.themes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ContrastWarning describes one color pair below the WCAG AA threshold
// for normal text (4.5:1). It is informational, not a load error,
// so users keep full control over their themes.
type ContrastWarning struct {
	Theme      string
	Foreground string
	Background string
	Ratio      float64
}

func (w ContrastWarning) String() string {
	return fmt.Sprintf("theme %q: %s on %s has contrast %.2f:1 (WCAG AA requires 4.5:1)",
		w.Theme, w.Foreground, w.Background, w.Ratio)
}

// criticalPairs lists the foreground/background color pairs that must
// reach WCAG AA contrast for the rendered page to remain readable.
// Keys reference theme.Colors[…] entries.
var criticalPairs = []struct {
	fg, bg string
}{
	{"text", "bg"},
	{"text", "cardBg"},
	{"textMuted", "cardBg"},
	{"runtime", "cardBg"},
	{"apiLink", "cardBg"},
	{"successText", "successBg"},
	{"jsonText", "jsonBg"},
	{"jsonRuntime", "jsonBg"},
	{"startText", "startBg"},
	{"endText", "endBg"},
	{"badgeText", "badgeBg"},
}

// Audit runs the contrast checker over every theme in the registry
// and returns the list of pairs below the WCAG AA threshold.
func (r *Registry) Audit() []ContrastWarning {
	var warnings []ContrastWarning
	for _, name := range r.List() {
		t := r.themes[name]
		for _, p := range criticalPairs {
			fg, ok1 := t.Colors[p.fg]
			bg, ok2 := t.Colors[p.bg]
			if !ok1 || !ok2 {
				continue
			}
			ratio := contrastRatio(fg, bg)
			if ratio < 4.5 {
				warnings = append(warnings, ContrastWarning{
					Theme:      name,
					Foreground: p.fg,
					Background: p.bg,
					Ratio:      ratio,
				})
			}
		}
	}
	return warnings
}

// parseBytes parses a themes YAML payload and returns the validated
// themes plus the (possibly empty) declared default name.
func parseBytes(data []byte, source string) ([]Theme, string, error) {
	var raw rawFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, "", fmt.Errorf("%s: %w", source, err)
	}
	seen := make(map[string]bool, len(raw.Themes))
	themes := make([]Theme, 0, len(raw.Themes))
	for i, rt := range raw.Themes {
		if rt.Name == "" {
			return nil, "", fmt.Errorf("%s: themes[%d] has no name", source, i)
		}
		if seen[rt.Name] {
			return nil, "", fmt.Errorf("%s: duplicate theme name %q", source, rt.Name)
		}
		seen[rt.Name] = true
		t, err := finalize(rt)
		if err != nil {
			return nil, "", fmt.Errorf("%s: theme %q: %w", source, rt.Name, err)
		}
		themes = append(themes, t)
	}
	return themes, raw.Default, nil
}

func finalize(rt rawTheme) (Theme, error) {
	font := rt.Font
	if font == "" {
		font = "sans"
	}
	if !validFonts[font] {
		return Theme{}, fmt.Errorf(
			"invalid font %q (only sans, serif, mono are allowed; see .agents/rules/eco-design.md)",
			font,
		)
	}
	shape := rt.Shape
	if shape == "" {
		shape = "rounded"
	}
	if !validShapes[shape] {
		return Theme{}, fmt.Errorf("invalid shape %q (allowed: rounded, square)", shape)
	}
	if rt.Colors == nil {
		return Theme{}, errors.New("missing colors")
	}
	for _, key := range requiredColors {
		v, ok := rt.Colors[key]
		if !ok {
			return Theme{}, fmt.Errorf("missing required color %q", key)
		}
		if !hexColorRe.MatchString(v) {
			return Theme{}, fmt.Errorf("color %q is not a valid hex value: %q", key, v)
		}
	}
	for key, v := range rt.Colors {
		if !hexColorRe.MatchString(v) {
			return Theme{}, fmt.Errorf("color %q is not a valid hex value: %q", key, v)
		}
	}
	return Theme{
		Name:   rt.Name,
		Font:   font,
		Shape:  shape,
		Colors: rt.Colors,
	}, nil
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return "<none>"
	}
	out := names[0]
	for _, n := range names[1:] {
		out += ", " + n
	}
	return out
}

// FontStack returns the CSS font-family value for a given font name.
// Only system stacks are supported (see .agents/rules/eco-design.md).
func FontStack(name string) string {
	switch name {
	case "serif":
		return `ui-serif, Georgia, "Times New Roman", serif`
	case "mono":
		return `ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace`
	default:
		return `ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif`
	}
}

// ---- WCAG contrast helpers ----

// contrastRatio returns the WCAG 2.x contrast ratio between two hex
// colors (any of #RGB, #RRGGBB, #RRGGBBAA, alpha is ignored).
func contrastRatio(a, b string) float64 {
	la := relativeLuminance(a)
	lb := relativeLuminance(b)
	hi, lo := la, lb
	if lb > la {
		hi, lo = lb, la
	}
	return (hi + 0.05) / (lo + 0.05)
}

func relativeLuminance(hex string) float64 {
	r, g, b := parseHex(hex)
	rl := channelToLinear(float64(r) / 255.0)
	gl := channelToLinear(float64(g) / 255.0)
	bl := channelToLinear(float64(b) / 255.0)
	return 0.2126*rl + 0.7152*gl + 0.0722*bl
}

func channelToLinear(c float64) float64 {
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func parseHex(hex string) (r, g, b uint8) {
	s := hex
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	switch len(s) {
	case 3:
		r = nibble(s[0]) * 17
		g = nibble(s[1]) * 17
		b = nibble(s[2]) * 17
	case 6, 8:
		r = nibble(s[0])<<4 | nibble(s[1])
		g = nibble(s[2])<<4 | nibble(s[3])
		b = nibble(s[4])<<4 | nibble(s[5])
	}
	return
}

func nibble(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}
