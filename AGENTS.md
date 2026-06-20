# AGENTS.md

Entry point for any agent (Claude Code, Claude Cowork, Cursor, Copilot,
Aider, …) working on this project.

## TL;DR for a fresh agent

1. Read [`.agents/rules/`](./.agents/rules/) (eco-design, accessibility,
   code-style) **before touching any code**. These rules are
   non-negotiable.
2. The truth-source for decisions, history, and roadmap is
   [`Plan.md`](./Plan.md). Read it for the "why" of any design choice.
3. Run `go test ./...` and `go vet ./...`: both must stay clean.
4. All committed text is in **English** (see Language rule below).

## Hard rules, read before doing anything

### Language

**All project documentation is written in English.** This includes
`README.md`, `Plan.md`, every file under `.agents/rules/`, this file,
inline Markdown in code, and every new doc added going forward. Reason:
open source Apache 2.0 project aiming for ecosystem adoption, French
docs would silently exclude most contributors. Code, identifiers, and
comments were already English; this just aligns the docs with that.

Conversations with the user may happen in any language, only **what
gets committed to the repo** must be English.

### Project-specific rules

Non-negotiable rules live in [`.agents/rules/`](./.agents/rules/):

- [`eco-design.md`](./.agents/rules/eco-design.md), eco-design
  constraints (minimise resources, requests, dependencies). **Hard
  example**: fonts are limited to `sans` / `serif` / `mono` system
  stacks; Google Fonts and `@font-face` are forbidden.
- [`accessibility.md`](./.agents/rules/accessibility.md), WCAG 2.2 AA
  target, semantic HTML, contrast checks. **Hard example**: every
  built-in theme must pass 4.5:1 contrast on the 10 critical
  foreground/background pairs (enforced by `theme.Audit()` + tests).
- [`code-style.md`](./.agents/rules/code-style.md), Go conventions,
  comments policy (default: no comments), tests, git.

Read these **before** implementing anything touching HTML output,
templates, themes, or external dependencies.

## Project context

### What it is

`arazzo-maestro` is a Go CLI that turns
[Arazzo](https://spec.openapis.org/arazzo/latest.html) workflow YAML
specifications into:

- structured validation findings (`lint` subcommand)
- standalone HTML pages per workflow (`view` subcommand)
- runnable tests: end-to-end (`test gen e2e` / `test run e2e`, Hurl)
  and load/performance (`test gen perf`, k6)

Originally a Python POC (now removed), migrated to Go in May 2026.

### Stack

- **Go 1.23+** (no cgo)
- [`spf13/cobra`](https://github.com/spf13/cobra), CLI framework
- [`gopkg.in/yaml.v3`](https://pkg.go.dev/gopkg.in/yaml.v3), YAML parsing
  with `yaml.Node` (we walk the tree manually to preserve insertion
  order of `outputs` blocks)
- [`santhosh-tekuri/jsonschema/v5`](https://github.com/santhosh-tekuri/jsonschema), 
  pure-Go JSON Schema validator (first pass of the linter)
- [`pb33f/libopenapi`](https://github.com/pb33f/libopenapi), pure-Go
  OpenAPI parser used by `internal/oasresolver` to resolve
  `operationId` → `(method, path, server, parameters)` for both the
  cross-file linter pass and the Hurl test generator (adopted in #28)
- Stdlib `html/template` for HTML rendering
- Stdlib `embed` for templates + JSON Schema + built-in themes
- Tailwind CSS via CDN (the only external runtime dep of the generated
  HTML; to be internalised later, eco-design rule R8)

No JS framework. No Node. No cgo. No Python.

### Repository layout

```
cmd/arazzo-maestro/          CLI entry point (Cobra)
internal/
├── model/                   Pure data types
├── parser/                  YAML → model.ArazzoDocument
├── linter/                  Three-pass validator:
│   ├── schema.go            JSON Schema (official OAI, embedded)
│   ├── linter.go            Semantic rules (uniqueness, $steps refs)
│   ├── crossfile.go         sourceDescriptions checks (operationId
│   │                        exists?), built on internal/oasresolver
│   └── schemas/arazzo-1.0.json   Embedded schema (patched at load)
├── oasresolver/             OpenAPI source loader (pb33f/libopenapi):
│                            operationId → method/path/server/params
├── hurlgen/                 model.Workflow + oasresolver → .hurl e2e
│                            tests ([Captures], [Asserts], {{baseUrl}})
├── k6gen/                   model.Workflow + oasresolver → .k6.js perf
│                            tests (http.request, check, BASE_URL env)
├── theme/                   Theme registry, validation, WCAG audit
│   └── themes/builtin.yml   Built-in light + dark themes
└── renderer/                model + theme → standalone HTML
    └── templates/           workflow.html + index.html (embedded)
examples/                    Naming convention:
├── *.arazzo.yaml            Arazzo workflow files (auto-picked by `make dist` / `make lint`)
├── *.yaml                   OpenAPI contracts referenced by sourceDescriptions[].url
│
├── shop.arazzo.yaml         Sample Arazzo, happy path + onFailure retry
├── shop-openapi.yaml        OpenAPI contract for shop.arazzo.yaml (Vacuum 100/100)
├── checkout-branching.arazzo.yaml   Sample Arazzo, onSuccess/onFailure goto branching
└── checkout-branching-api.yaml      OpenAPI contract for checkout-branching.arazzo.yaml
.agents/rules/               Non-negotiable rules (see above)
themes.yml.example           Annotated theme template
Makefile                     `make help` lists targets
Plan.md                      Decisions, history, roadmap, feature specs
LICENSE                      Apache 2.0
```

**Adding a new example**: name it `examples/<something>.arazzo.yaml`.
The Makefile's `dist` and `lint` targets glob `examples/*.arazzo.yaml`
and pick it up automatically, no changes needed elsewhere.

Dependency graph (no cycles): `model` → ∅, `parser` → `model`,
`oasresolver` → libopenapi, `linter` → `parser` + `model` + `oasresolver`,
`hurlgen` → `model` + `oasresolver`, `k6gen` → `model` + `oasresolver`,
`theme` → ∅, `renderer` → `model` + `theme`, `cmd` → all.

### State as of 2026-06-02

| Area | Status | Where |
|---|---|---|
| Parser (YAML → model, ordered outputs) | ✅ done | `internal/parser/` |
| Renderer (HTML output) | ✅ done | `internal/renderer/` |
| CLI (`lint`, `view`, `test`) | ✅ done | `cmd/arazzo-maestro/` |
| Theme system (light, dark, user `themes.yml`, WCAG audit) | ✅ done | `internal/theme/` |
| Linter pass 1: JSON Schema | ✅ done | `internal/linter/schema.go` |
| Linter pass 2: semantic rules | ✅ done | `internal/linter/linter.go` |
| Linter pass 3: cross-file (operationId in OpenAPI) | ✅ done | `internal/linter/crossfile.go` |
| OpenAPI source resolution (libopenapi) | ✅ done (#28) | `internal/oasresolver/` |
| Test gen: e2e → Hurl (`test gen/run e2e`) | ✅ done (#21, merged) | `internal/hurlgen/` |
| Test gen: perf → k6 (`test gen perf`) | ✅ done (#22, on `feat/k6-gen-22`) | `internal/k6gen/` |
| Test coverage | ✅ ≥80 % all packages | `*_test.go` |
| README | ✅ structurally done, ⏭️ visual hero pending | `README.md` |
| CI (GitHub Actions) | ✅ done | `.github/workflows/ci.yml` |
| Releases (goreleaser, cosign-signed, SBOM) | ✅ v0.1.0 shipped | `.github/workflows/release.yml` |
| OpenSSF Best Practices badge | ✅ Phases 1-2 done; registered on bestpractices.dev | see Plan.md |
| Logo / demo GIF / screenshots | ❌ pending | see Plan.md README polish |

See [`Plan.md`](./Plan.md) for the full roadmap, including the OpenSSF
3-phase plan and the README polish checklist.

## Useful commands

A Makefile codifies the everyday commands. Glob targets (`lint`,
`dist`) iterate over **every** `examples/*.arazzo.yaml`, so adding a
new example requires no command change.

```sh
make help                            # list available targets
make build                           # build bin/arazzo-maestro
make test                            # go test ./...
make vet                             # go vet ./...
make lint                            # arazzo-maestro lint examples/*.arazzo.yaml
make dist                            # render every example into dist/<workflow>/{light,dark}/
make hurl                            # generate Hurl e2e tests under examples/generated/e2e/hurl/
make perf                            # generate k6 perf tests under examples/generated/perf/k6/
make clean                           # rm -rf dist bin examples/generated

# One-off invocations still work:
go test -cover ./...                                        # coverage per package
go run ./cmd/arazzo-maestro view --list-themes              # list available themes
go run ./cmd/arazzo-maestro lint examples/shop.arazzo.yaml  # lint a single file
go mod tidy                                                 # keep go.mod clean
```

## Verification before declaring a change done

- `make test vet` clean
- `make lint` returns `OK` on every `examples/*.arazzo.yaml`
- `make dist` regenerates every example without errors; spot-check at
  least one `dist/<workflow>/light/index.html` opens in a browser
- If you touched a theme: `theme.Audit()` produces no warnings on
  built-ins (covered by `TestBuiltinThemesPassWCAG`)
- If you touched the linter: all linter tests still pass

## Common pitfalls, don't do this

- ❌ **Don't add a Google Font**: violates eco-design R1/R2 (see rule)
- ❌ **Don't shell out to a Node-based validator**: kills our
  `FROM scratch` Docker promise, kills our positioning
- ❌ **Don't add cgo**: breaks cross-compilation, see eco-design R6
- ❌ **Don't replace Tailwind classes with inline styles** for theming
, we use CSS custom properties driven by the theme; layout classes
  stay Tailwind
- ❌ **Don't loosen the JSON Schema strictness**: the version-pattern
  patch is explicit, documented, and the only one
- ❌ **Don't add a flag to the linter for "skip cross-file"** unless
  asked, the current design is deliberately all-on (see Plan.md
  decision)
- ❌ **Don't break the rule that `--theme light` (or no flag) renders
  identically to the current `examples/shop.arazzo.yaml` output**: golden
  expectation in `internal/renderer/renderer_test.go`. The same applies to
  `--layout`: portrait is the default and its output must stay
  byte-identical; `landscape` is strictly opt-in (adds a body class +
  scoped CSS, no portrait change)

## When in doubt

1. Re-read the relevant rule in `.agents/rules/`
2. Check `Plan.md` for prior context on the topic
3. If a rule blocks a user request, **flag the conflict** in the
   conversation before acting, never silently bypass a rule
4. If no rule covers the topic and the decision has impact, ask
   before deciding

## Reporting back

When you finish a task, report:

- What you changed (files + brief reason)
- Whether `go test ./...`, `go vet ./...`, and the lint+view e2e on
  `examples/shop.arazzo.yaml` are all clean
- Anything you noticed but didn't fix (so the user / next agent can
  pick it up)
