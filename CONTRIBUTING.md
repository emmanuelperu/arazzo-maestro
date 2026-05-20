# Contributing to arazzo-maestro

Thanks for considering a contribution. This guide should cover everything
you need to file an issue or send a pull request.

## Ground rules

Before changing code, read these three rule files — they are
non-negotiable for any contribution:

1. [`.agents/rules/eco-design.md`](./.agents/rules/eco-design.md) —
   why the generated HTML has 1 network request and uses only system
   fonts.
2. [`.agents/rules/accessibility.md`](./.agents/rules/accessibility.md) —
   why every built-in theme passes WCAG 2.2 AA on its critical colour
   pairs.
3. [`.agents/rules/code-style.md`](./.agents/rules/code-style.md) —
   Go conventions, comments policy, English-only documentation.

For high-level project context, decisions, and roadmap:

- [`Plan.md`](./Plan.md) — full history and the live roadmap.
- [`AGENTS.md`](./AGENTS.md) — entry point for any coding agent (and a
  good orientation for humans, too).

## Setting up a development environment

Requirements:

- **Go 1.23+** (see `go.mod` for the exact version we test against).
- **GNU Make** for the `make` targets — any recent macOS or Linux
  shell ships one.
- *(Optional)* [`golangci-lint`](https://golangci-lint.run/) for local
  linting:
  ```sh
  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
  ```

Clone, build, run the demos:

```sh
git clone https://github.com/emmanuelperu/arazzo-maestro.git
cd arazzo-maestro
make help          # list available targets
make build         # build bin/arazzo-maestro
make dist          # render every examples/*.arazzo.yaml
open dist/shop/light/index.html
```

## The development loop

```sh
make test          # go test ./...
make vet           # go vet ./...
golangci-lint run  # full lint suite (matches the CI job)
make lint          # lint every examples/*.arazzo.yaml with the binary
make dist          # render every examples/*.arazzo.yaml into dist/
```

The Makefile's `dist` and `lint` targets glob `examples/*.arazzo.yaml`
— adding a new example file requires no change to the build command.

## Pull request checklist

Before sending a PR, please confirm:

- [ ] **Tests pass**: `make test` is green.
- [ ] **Vet clean**: `make vet` is silent.
- [ ] **Lint clean**: `golangci-lint run` reports zero findings against
  [`.golangci.yml`](./.golangci.yml).
- [ ] **Example lint clean**: `make lint` reports no issues on every
  `examples/*.arazzo.yaml`.
- [ ] **Render works**: `make dist` regenerates every example without
  errors.
- [ ] **A11y unchanged**: if you touched a theme palette or a
  template, the WCAG audit
  (`TestBuiltinThemesPassWCAG` and the related theme tests) still
  passes.
- [ ] **Eco-design unchanged**: if you touched the HTML output, attach
  the byte count (raw and gzipped) of one
  `examples/*.arazzo.yaml`'s rendering to the PR description.
  Regressions > 10 % require explicit discussion.
- [ ] **English only**: code, comments, and documentation are in
  English (see code-style rule).

## Conventions

### Commits

- One commit = one intent. Split refactors from feature work.
- Imperative present, subject ≤ 70 characters.
  - Good: `linter: catch goto targets that don't exist`
  - Less good: `Fixed some stuff in the linter`

### Tests

- Use the stdlib `testing` package. We rely on
  [`google/go-cmp`](https://github.com/google/go-cmp) only where
  structured diffs are unavoidable.
- Table-driven tests for sets of similar cases; one `t.Run` per case
  otherwise.
- Golden files for HTML rendering — one per theme, byte-for-byte
  comparison.

### Comments

- Default: **don't comment**. Identifier names should speak for
  themselves.
- Comment only when the **why** is non-obvious (subtle invariant,
  workaround for an external bug, hidden constraint).
- Go doc comments only for exported symbols, one short sentence, start
  with the symbol name.

### Dependencies

- Adding a dependency requires a justification in the commit message
  (what it gives us, why the stdlib can't).
- Prefer stdlib when the effort gap is < 50 lines.
- **No cgo.** It breaks cross-compilation and bloats the
  `FROM scratch` Docker image we ship.

## Reporting issues

- **Bugs**: open an issue with reproduction steps, expected vs actual,
  and the `arazzo-maestro --version` output.
- **Feature requests**: please cross-reference the relevant section
  of the [Arazzo specification](https://spec.openapis.org/arazzo/latest.html)
  if the request relates to spec coverage, or to
  [`Plan.md`](./Plan.md) if it overlaps an open decision.
- **Security**: see [`SECURITY.md`](./SECURITY.md) — please do not
  file public issues for vulnerability reports.

## Code of conduct

This project follows the
[Contributor Covenant](https://www.contributor-covenant.org/) v2.1.
By participating, you agree to uphold this code.

To report unacceptable behaviour privately, please
[open a GitHub Security Advisory](https://github.com/emmanuelperu/arazzo-maestro/security/advisories/new)
on this repository — the advisory mechanism handles confidential
reports without requiring an email exchange.

## License

By contributing, you agree that your contributions will be licensed
under the [Apache License 2.0](./LICENSE), the same terms that cover
the rest of the project.
