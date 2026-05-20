package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBuiltin(t *testing.T) {
	r, err := LoadBuiltin()
	if err != nil {
		t.Fatalf("LoadBuiltin: %v", err)
	}
	if r.Default != BuiltinDefault {
		t.Errorf("Default = %q, want %q", r.Default, BuiltinDefault)
	}
	names := r.List()
	if len(names) < 2 {
		t.Errorf("expected at least 2 built-in themes, got %v", names)
	}
	for _, want := range []string{"light", "dark"} {
		if _, err := r.Resolve(want); err != nil {
			t.Errorf("Resolve(%q): %v", want, err)
		}
	}
}

func TestBuiltinThemesPassWCAG(t *testing.T) {
	r, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	warnings := r.Audit()
	for _, w := range warnings {
		t.Errorf("built-in theme contrast failure: %s", w)
	}
}

func TestResolveUsesDefaultWhenEmpty(t *testing.T) {
	r, _ := LoadBuiltin()
	got, err := r.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\"): %v", err)
	}
	if got.Name != BuiltinDefault {
		t.Errorf("Resolve(\"\") = %q, want %q", got.Name, BuiltinDefault)
	}
}

func TestResolveUnknownTheme(t *testing.T) {
	r, _ := LoadBuiltin()
	_, err := r.Resolve("ghost")
	if err == nil {
		t.Fatal("expected error for unknown theme")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error message = %q, want 'not found'", err)
	}
}

func TestMergeFileOverridesBuiltinAndAddsTheme(t *testing.T) {
	tmp := writeTempThemes(t, `
default: dark
themes:
  - name: dark
    font: sans
    shape: square
    colors:
      bg: "#000000"
      bgGrid: "#222222"
      text: "#ffffff"
      textMuted: "#dddddd"
      heading: "#ffffff"
      cardBg: "#111111"
      cardBorder: "#222222"
      cardHeaderBg: "#191919"
      runtime: "#ffb86c"
      apiLink: "#8be9fd"
      startBg: "#001100"
      startBorder: "#003300"
      startText: "#ffffff"
      startMuted: "#88ff88"
      endBg: "#000011"
      endBorder: "#000033"
      endText: "#ffffff"
      endMuted: "#8888ff"
      jsonBg: "#000000"
      jsonText: "#eeeeee"
      jsonRuntime: "#ffb86c"
      successBg: "#001100"
      successBorder: "#003300"
      successText: "#88ff88"
      rail: "#444444"
      badgeBg: "#001133"
      badgeBorder: "#003366"
      badgeText: "#88ccff"
      footer: "#888888"

  - name: brand
    font: serif
    shape: square
    colors:
      bg: "#fafaf7"
      bgGrid: "#e7e5e0"
      text: "#1a1a1a"
      textMuted: "#555555"
      heading: "#000000"
      cardBg: "#ffffff"
      cardBorder: "#d4d4d0"
      cardHeaderBg: "#fafaf7"
      runtime: "#7e22ce"
      apiLink: "#1d4ed8"
      startBg: "#fafaf7"
      startBorder: "#2d6a4f"
      startText: "#1b4332"
      startMuted: "#c8e6c9"
      endBg: "#1f2937"
      endBorder: "#374151"
      endText: "#ffffff"
      endMuted: "#cbd5e1"
      jsonBg: "#1c1917"
      jsonText: "#fafaf9"
      jsonRuntime: "#fcd34d"
      successBg: "#f0f7f0"
      successBorder: "#86b386"
      successText: "#2d5016"
      rail: "#888888"
      badgeBg: "#dbeafe"
      badgeBorder: "#bfdbfe"
      badgeText: "#1d4ed8"
      footer: "#666666"
`)
	r, _ := LoadBuiltin()
	if err := r.MergeFile(tmp); err != nil {
		t.Fatalf("MergeFile: %v", err)
	}
	if r.Default != "dark" {
		t.Errorf("Default = %q, want dark", r.Default)
	}
	dark, _ := r.Resolve("dark")
	if dark.Shape != "square" {
		t.Errorf("dark.Shape = %q, want square (user override)", dark.Shape)
	}
	brand, err := r.Resolve("brand")
	if err != nil {
		t.Fatalf("Resolve(brand): %v", err)
	}
	if brand.Font != "serif" {
		t.Errorf("brand.Font = %q, want serif", brand.Font)
	}
}

func TestMergeFileRejectsUnknownDefault(t *testing.T) {
	tmp := writeTempThemes(t, `default: ghost`)
	r, _ := LoadBuiltin()
	err := r.MergeFile(tmp)
	if err == nil || !strings.Contains(err.Error(), "default theme") {
		t.Errorf("expected 'default theme … is not defined', got %v", err)
	}
}

func TestRejectsInvalidFont(t *testing.T) {
	tmp := writeTempThemes(t, `
themes:
  - name: bad
    font: Poppins
    colors:
      bg: "#ffffff"
      bgGrid: "#cccccc"
      text: "#000000"
      textMuted: "#444444"
      heading: "#000000"
      cardBg: "#ffffff"
      cardBorder: "#dddddd"
      cardHeaderBg: "#ffffff"
      runtime: "#7e22ce"
      apiLink: "#1d4ed8"
      startBg: "#ffffff"
      startBorder: "#10b981"
      startText: "#0f172a"
      startMuted: "#d1fae5"
      endBg: "#0f172a"
      endBorder: "#334155"
      endText: "#ffffff"
      endMuted: "#cbd5e1"
      jsonBg: "#0f172a"
      jsonText: "#e2e8f0"
      jsonRuntime: "#fcd34d"
      successBg: "#ecfdf5"
      successBorder: "#a7f3d0"
      successText: "#065f46"
      rail: "#94a3b8"
      badgeBg: "#dbeafe"
      badgeBorder: "#bfdbfe"
      badgeText: "#1d4ed8"
      footer: "#64748b"
`)
	r, _ := LoadBuiltin()
	err := r.MergeFile(tmp)
	if err == nil || !strings.Contains(err.Error(), "invalid font") {
		t.Errorf("expected 'invalid font', got %v", err)
	}
	if !strings.Contains(err.Error(), "eco-design") {
		t.Errorf("error should point at the rule (.agents/rules/eco-design.md), got %v", err)
	}
}

func TestRejectsInvalidHexColor(t *testing.T) {
	tmp := writeTempThemes(t, `
themes:
  - name: bad
    colors:
      bg: "not-a-color"
`)
	r, _ := LoadBuiltin()
	err := r.MergeFile(tmp)
	if err == nil || !strings.Contains(err.Error(), "not a valid hex") {
		t.Errorf("expected hex validation error, got %v", err)
	}
}

func TestContrastRatioKnownValues(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"#000000", "#ffffff", 21},
		{"#ffffff", "#000000", 21},
		{"#777777", "#ffffff", 4.48},
	}
	for _, c := range cases {
		got := contrastRatio(c.a, c.b)
		if abs(got-c.want) > 0.05 {
			t.Errorf("contrastRatio(%s, %s) = %.3f, want ~%.2f", c.a, c.b, got, c.want)
		}
	}
}

func TestAuditFlagsLowContrastCustomTheme(t *testing.T) {
	// Light grey text on white fails AA at ~2.85:1.
	tmp := writeTempThemes(t, `
themes:
  - name: bad
    colors:
      bg: "#ffffff"
      bgGrid: "#eeeeee"
      text: "#aaaaaa"
      textMuted: "#cccccc"
      heading: "#aaaaaa"
      cardBg: "#ffffff"
      cardBorder: "#dddddd"
      cardHeaderBg: "#ffffff"
      runtime: "#aaaaaa"
      apiLink: "#aaaaaa"
      startBg: "#aaaaaa"
      startBorder: "#aaaaaa"
      startText: "#ffffff"
      startMuted: "#cccccc"
      endBg: "#aaaaaa"
      endBorder: "#aaaaaa"
      endText: "#ffffff"
      endMuted: "#cccccc"
      jsonBg: "#aaaaaa"
      jsonText: "#cccccc"
      jsonRuntime: "#aaaaaa"
      successBg: "#ffffff"
      successBorder: "#eeeeee"
      successText: "#aaaaaa"
      rail: "#aaaaaa"
      badgeBg: "#ffffff"
      badgeBorder: "#eeeeee"
      badgeText: "#aaaaaa"
      footer: "#aaaaaa"
`)
	r, _ := LoadBuiltin()
	if err := r.MergeFile(tmp); err != nil {
		t.Fatal(err)
	}
	warnings := r.Audit()
	hasBadWarning := false
	for _, w := range warnings {
		if w.Theme == "bad" {
			hasBadWarning = true
			if w.Ratio >= 4.5 {
				t.Errorf("warning emitted but ratio %.2f is above threshold", w.Ratio)
			}
		}
	}
	if !hasBadWarning {
		t.Errorf("expected at least one contrast warning for 'bad' theme, got %v", warnings)
	}
}

func TestContrastWarningString(t *testing.T) {
	w := ContrastWarning{Theme: "x", Foreground: "text", Background: "bg", Ratio: 3.2}
	s := w.String()
	for _, want := range []string{"x", "text", "bg", "3.20", "WCAG"} {
		if !strings.Contains(s, want) {
			t.Errorf("ContrastWarning.String() missing %q in %q", want, s)
		}
	}
}

func TestFontStackKnownValues(t *testing.T) {
	if !strings.Contains(FontStack("sans"), "ui-sans-serif") {
		t.Errorf("FontStack(sans) should contain ui-sans-serif")
	}
	if !strings.Contains(FontStack("serif"), "Georgia") {
		t.Errorf("FontStack(serif) should contain Georgia")
	}
	if !strings.Contains(FontStack("mono"), "ui-monospace") {
		t.Errorf("FontStack(mono) should contain ui-monospace")
	}
}

func writeTempThemes(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "themes.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write tmp themes: %v", err)
	}
	return path
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
