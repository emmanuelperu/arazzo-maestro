# Accessibility rules

> **Target**: WCAG 2.2 level AA on the generated pages **and** on the
> CLI output. The render must be usable by someone navigating with
> the keyboard, using a screen reader, or running their OS in high
> contrast / enlarged text mode.

## Rules

### A1, Semantic HTML first, classes second

- One page = one `<main>`
- Heading hierarchy respected: `<h1>` for the workflow name, then
  `<h2>` for START / END, `<h3>` for each step. No skipping
  (h1 → h3).
- `<section>` for START / step / END blocks
- `<header>` / `<footer>` for structural zones
- `<ul>` for lists (inputs, outputs, parameters, success criteria)
- `<code>` for technical identifiers and runtime expressions

A `<div class="text-xl font-bold">` instead of an `<h2>` is an
**accessibility bug**, not a styling decision.

### A2, `lang` mandatory on `<html>`

`<html lang="en">` today (output is in English). If we internationalise
later, switch dynamically based on the generation locale.

### A3, Text/background contrast ≥ 4.5:1 (WCAG AA)

All built-in themes must reach:

- Normal text on background: ≥ 4.5:1
- Bold text / ≥ 18px: ≥ 3:1
- UI components (card borders, focus indicators): ≥ 3:1

To verify across **all** colour/background pairs:

- `text` on `bg`
- `text` on `cardBg`
- `textMuted` on `cardBg`
- `runtime` on `bg` **and** `cardBg`
- `apiLink` on `cardBg`
- `successText` on `successBg`
- `jsonText` on `jsonBg`

A user custom theme must trigger a **warning** at load time if any of
these ratios falls below the threshold (not a hard block, the user
remains in control).

### A4, No info conveyed by colour alone

- A failing step is not distinguished only by red, add an icon /
  text label
- The "API" badge is text, not just a coloured pill, ✅ already the
  case in the current template
- Runtime expressions (purple) are also rendered in monospace
  `<code>`: colour is reinforcement, not the sole signal

### A5, Visible focus and keyboard navigation

Every interactive element (workflow links on `index.html`, eventually
"copy" buttons, etc.) must have a visible focus indicator with ≥ 3:1
contrast.

No `outline: none` without an explicit replacement.

### A6, Fluid text sizes

- Body: minimum `16px` (1rem), **never** below 14px
- Prefer `rem`/`em` over `px` for text sizes (honours browser zoom and
  OS preferences)
- Layout must stay functional at 200 % zoom (WCAG 1.4.4)

### A7, `prefers-reduced-motion`

If a transition is added (focus, hover), wrap it in:

```css
@media (prefers-reduced-motion: no-preference) {
  .card { transition: box-shadow 0.2s; }
}
```

### A8, Screen readers: `aria-label` where useful

- Decorative vertical arrows (`vertical-arrow`) are pure CSS →
  `aria-hidden="true"` if they appear in the DOM
- Decorative icons (▶, ■, 🔗, ←, →) are decorative → wrap in
  `<span aria-hidden="true">`
- Surrounding text content is what the screen reader relies on

### A9, CLI output is accessible too

- No essential info conveyed only by ANSI colour
- Prefix severity levels with a word (`error:`, `warning:`), not just
  a red/yellow colour
- Respect `NO_COLOR` (env var convention: disables colour)
- Emit parseable messages: one finding per line, structured

### A10, Test with real tools

Before merging a visual refactor, validate:

- `pa11y` or `axe-core` on the generated HTML (`examples/shop.arazzo.yaml`)
- Full keyboard navigation on `index.html` → `workflow.html`
- Screen reader inspection (VoiceOver on macOS is enough for a POC)

Attach the report to the PR.

## Anti-patterns to refuse

- "`<div onclick="…">` to skip a button" → use `<button>` or `<a>`
- "Light grey text on white, subtle and pretty" → check contrast,
  almost always < 4.5:1 if subtle
- "An icon alone is enough, it's universal" → A4, double up with text
- "`outline: none` on links is cleaner" → A5, breaking focus makes
  the page unusable with a keyboard

## Recommended tools

- **Contrast**: https://webaim.org/resources/contrastchecker/ or
  `npx wcag-contrast` for scripting
- **Auto-audit**: `pa11y https://...` or `axe-core` via Playwright
- **Manual**: VoiceOver (macOS), NVDA (Windows), Tab navigation
