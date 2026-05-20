# Code conventions

## Language

- **All documentation in this repo is written in English.** README,
  Plan.md, files in this directory, inline Markdown, everything that
  gets committed. Reason: open source project, international audience.
- **Code, identifiers, and comments**: English (this was already the
  case).
- **User-facing CLI messages**: English.
- Conversations with the user can happen in any language, only
  committed artefacts must be English.

## Go

- `gofmt` is non-negotiable (enforced by `go build`)
- `goimports` for import ordering
- `go vet` must pass; eventually `golangci-lint` (see Plan.md
  industrialisation)
- Errors: `fmt.Errorf("context: %w", err)` to wrap; no panics on the
  normal path (only for impossible invariants)
- Tests: table-driven when several similar cases exist, otherwise one
  `t.Run` per case
- No external test framework: stdlib `testing` + `go-cmp` for
  structured diffs

## Comments

- Default: **don't comment**. Identifier names should speak for
  themselves.
- Comment only when the **why** is non-obvious: subtle invariant,
  workaround for an external bug, hidden constraint.
- Go doc comments only for exported symbols, short (one sentence),
  starting with the symbol name (`// FooBar does …`).
- No `// TODO:` without an associated ticket.

## Structure

- Standard Go layout (`cmd/`, `internal/`)
- One package per clear responsibility (`parser`, `linter`, `renderer`,
  `theme`, `model`)
- Business structs live in `internal/model`; other packages consume
  them without mutating
- No catch-all `internal/utils`

## Tests

- Cover happy paths plus at least one error case per public function
- Golden files for HTML rendering: one frozen file per built-in theme,
  compared byte-for-byte
- End-to-end CLI tests live in `cmd/.../*_test.go`

## Git

- No automatic `Co-Authored-By` (see global instructions)
- One commit = one intent (no mixing refactor + feature)
- Messages: imperative present, subject ≤ 70 characters
- Check the current branch before committing; never directly on `main`
  for a feature

## Dependencies

- Adding a dependency requires a justification in the commit message
- Prefer the stdlib when the effort gap is < 50 lines
- No cgo (see eco-design rule R6)
