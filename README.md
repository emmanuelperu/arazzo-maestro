<!--
  Repo: https://github.com/emmanuelperu/arazzo-maestro

  REMAINING PLACEHOLDER: the OpenSSF Best Practices badge below still uses
  `projects/OWNER`: replace OWNER with the numeric project ID assigned
  when you register the project at https://www.bestpractices.dev (it is an
  ID, not a GitHub username).

  Some badges may resolve only after one-time setup:
  - Codecov / Go Report Card: need a one-time sign-in on those services.
  - Docker image: published at `ghcr.io/emmanuelperu/arazzo-maestro:0.0.1` since the v0.0.1 release; can also be built locally via `docker build --build-arg VERSION=0.0.1 -t arazzo-maestro:0.0.1 .` (see Docker section below).
-->

<p align="center">
  <img src="./docs/banner.webp" alt="arazzo-maestro: validator and HTML renderer for OpenAPI Arazzo workflows" width="480">
</p>

<h1 align="center">arazzo-maestro</h1>

<p align="center">
  <strong>Lint &amp; render Arazzo workflows. Single Go binary. Eco-designed. Accessible by default.</strong>
</p>

<p align="center">
  <a href="https://github.com/emmanuelperu/arazzo-maestro/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/emmanuelperu/arazzo-maestro/ci.yml?label=ci&logo=github"></a>
  <a href="https://codecov.io/gh/emmanuelperu/arazzo-maestro"><img alt="Coverage" src="https://img.shields.io/codecov/c/github/emmanuelperu/arazzo-maestro?logo=codecov"></a>
  <a href="https://goreportcard.com/report/github.com/emmanuelperu/arazzo-maestro"><img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/emmanuelperu/arazzo-maestro"></a>
  <a href="https://pkg.go.dev/github.com/emmanuelperu/arazzo-maestro"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/emmanuelperu/arazzo-maestro.svg"></a>
  <a href="https://github.com/emmanuelperu/arazzo-maestro/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/emmanuelperu/arazzo-maestro?logo=github&sort=semver"></a>
  <a href="https://github.com/emmanuelperu/arazzo-maestro/pkgs/container/arazzo-maestro"><img alt="Docker image" src="https://img.shields.io/badge/docker-FROM%20scratch-2496ED?logo=docker&logoColor=white"></a>
</p>

<p align="center">
  <a href="https://go.dev/dl/"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/emmanuelperu/arazzo-maestro?logo=go"></a>
  <a href="./LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Apache%202.0-blue.svg"></a>
  <a href="https://spec.openapis.org/arazzo/latest.html"><img alt="Arazzo" src="https://img.shields.io/badge/arazzo-1.0%20%7C%201.1-7e22ce"></a>
  <a href="./.agents/rules/accessibility.md"><img alt="WCAG 2.2 AA" src="https://img.shields.io/badge/WCAG-2.2%20AA-047857"></a>
  <a href="./.agents/rules/eco-design.md"><img alt="Eco-design" src="https://img.shields.io/badge/eco--design-by%20rule-2d5016"></a>
  <a href="https://www.bestpractices.dev/projects/OWNER"><img alt="OpenSSF Best Practices" src="https://www.bestpractices.dev/projects/OWNER/badge"></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/emmanuelperu/arazzo-maestro"><img alt="OpenSSF Scorecard" src="https://api.scorecard.dev/projects/github.com/emmanuelperu/arazzo-maestro/badge"></a>
</p>

<p align="center">
  <a href="https://github.com/emmanuelperu/arazzo-maestro/stargazers"><img alt="Stars" src="https://img.shields.io/github/stars/emmanuelperu/arazzo-maestro?style=social"></a>
  &nbsp;
  <a href="https://github.com/emmanuelperu/arazzo-maestro/commits/main"><img alt="Last commit" src="https://img.shields.io/github/last-commit/emmanuelperu/arazzo-maestro?logo=git&logoColor=white"></a>
  <a href="https://github.com/emmanuelperu/arazzo-maestro/issues"><img alt="Open issues" src="https://img.shields.io/github/issues/emmanuelperu/arazzo-maestro?logo=github"></a>
  <a href="https://github.com/emmanuelperu/arazzo-maestro/pulls"><img alt="Open PRs" src="https://img.shields.io/github/issues-pr/emmanuelperu/arazzo-maestro?logo=github"></a>
</p>

---

## What it does

`arazzo-maestro` is a CLI that turns [Arazzo](https://spec.openapis.org/arazzo/latest.html) workflow specifications into something useful for the rest of your team:

- **`lint`**: validate Arazzo files against the official JSON Schema, internal semantic rules (unique IDs, `$steps` references), and cross-file checks against the referenced OpenAPI contracts.
- **`view`**: generate a standalone HTML page per workflow, no server, no build, no JavaScript framework. Open in any browser, commit to a docs folder, ship to GitHub Pages.

```text
*.arazzo.yaml (Arazzo) ────┐
                           ├──►  arazzo-maestro lint   →  exit 0/1 + structured findings
*-openapi.yaml / *-api.yaml ┤
                           ├──►  arazzo-maestro view   →  dist/*.html  (standalone)
themes.yml (opt.)        ──┘
```

## Why it exists

| Need | What we do | What we don't try to be |
|---|---|---|
| Validate Arazzo files in CI | ✅ Single binary, deterministic, offline. `lint` → exit code + parseable findings | An IDE plugin |
| Share workflows with non-devs | ✅ Standalone HTML, no IDE, no auth | A live editor |
| Cross-file integrity (operationId exists?) | ✅ Reads `sourceDescriptions.url`, indexes operations, validates references | A full OpenAPI validator |
| Eco-designed output | ✅ 1 network request, system fonts, ~18 kB HTML | A pixel-perfect design system |
| Accessibility-first | ✅ WCAG 2.2 AA contrasts, semantic HTML, `aria-hidden` on decoratives | An a11y testing tool |

See ["What makes us different"](#what-makes-us-different) below for the longer take.

## Quick start

```bash
# Install (Go 1.23+)
go install github.com/emmanuelperu/arazzo-maestro/cmd/arazzo-maestro@latest

# Or build the Docker image locally (~5 MB FROM scratch)
docker build --build-arg VERSION=0.0.1 -t arazzo-maestro:0.0.1 .
# Mount cwd into /work AND set it as workdir, so relative paths
# (e.g. examples/shop.arazzo.yaml, dist/) resolve against your host cwd.
docker run --rm -v "$PWD":/work -w /work arazzo-maestro:0.0.1 \
  view examples/shop.arazzo.yaml

# Lint
arazzo-maestro lint examples/shop.arazzo.yaml

# Render every examples/*.arazzo.yaml in light + dark themes
make dist
open dist/shop/light/index.html
```

## Visual preview

The repo ships with two demo Arazzo files in [`examples/`](./examples):

- [`shop.arazzo.yaml`](./examples/shop.arazzo.yaml), happy-path checkout + a retry-on-failure path (showcases `onFailure: retry`). References [`shop-openapi.yaml`](./examples/shop-openapi.yaml) (scored 100/100 by [Vacuum](https://quobix.com/vacuum/)).
- [`checkout-branching.arazzo.yaml`](./examples/checkout-branching.arazzo.yaml), single payment step that branches via `onSuccess: goto` / `onFailure: goto` to a confirm or cancel step. References [`checkout-branching-api.yaml`](./examples/checkout-branching-api.yaml).

The `happy-path-checkout` workflow rendered by `view`, in the two built-in themes (click to enlarge):

| Light | Dark |
|---|---|
| [![happy-path-checkout workflow rendered by arazzo-maestro, light theme](./docs/screenshots/happy-light.webp)](./docs/screenshots/happy-light.webp) | [![happy-path-checkout workflow rendered by arazzo-maestro, dark theme](./docs/screenshots/happy-dark.webp)](./docs/screenshots/happy-dark.webp) |

```bash
# Render every examples/*.arazzo.yaml into dist/<workflow>/{light,dark}/
make dist
```

The Makefile iterates over `examples/*.arazzo.yaml`: adding a new file requires no change to the build command.

> 📂 **Live HTML demos in this repo**: [`dist/shop/{light,dark}/`](./dist/shop) and [`dist/checkout-branching/{light,dark}/`](./dist/checkout-branching). Regenerate with `make dist`.

## Features

### 🔍 Three-pass linter

1. **JSON Schema**: embedded official OAI Arazzo schema (1.0, patched at load to accept 1.0.x **and** 1.1.x). Catches types, required fields, enums, formats, regex.
2. **Semantic rules**: uniqueness of `workflowId` / `stepId`, resolution of `$steps.X.outputs.Y` references, no forward references between steps.
3. **Cross-file**: loads each `sourceDescriptions[].url` from local disk, indexes `operationId`s, validates that every step `operationId` (short form or qualified `$sourceDescriptions.<name>.<op>`) actually points at an operation that exists. HTTP/HTTPS URLs are intentionally refused (offline-first).

```text
$ arazzo-maestro lint examples/shop.arazzo.yaml
OK: examples/shop.arazzo.yaml, no issues found

$ arazzo-maestro lint broken.yaml
[error] arazzo: value does not match expected pattern '^1\.[01]\.\d+(-.+)?$'
[error] workflows[checkout].steps[create-order].operationId:
        operation "createOrder" not found in source "shop-api"
Error: 2 issue(s) found
```

### 🎨 Themes

Two themes ship built-in (`light` default, `dark`). Both pass WCAG 2.2 AA on all critical colour pairs, verified in tests.

Customise without rebuilding by dropping a `themes.yml` at the root of your project:

```yaml
# themes.yml, change the default with one line
default: dark
```

Or override / extend:

```yaml
default: corporate

themes:
  - name: corporate
    font: serif      # sans | serif | mono (system stacks only)
    shape: square    # rounded | square
    colors:
      bg: "#fafaf7"
      cardBg: "#ffffff"
      runtime: "#7e22ce"
      # …
```

Custom themes that drop below WCAG AA contrast emit warnings at load time. See [`themes.yml.example`](./themes.yml.example) for the full template, and [`internal/theme/themes/builtin.yml`](./internal/theme/themes/builtin.yml) for the reference palette.

### 📋 Arazzo coverage

What the tool **renders and validates** versus what is currently
out of scope. The linter catches everything that fails the official
JSON Schema; the table below tracks the visual rendering.

| Arazzo feature | Render | Notes |
|---|---|---|
| `info`, `sourceDescriptions`, `workflows[]` (top-level) | ✅ | Header frame + cross-file lint |
| `workflow.inputs` (JSON-Schema properties: name, type, default) | ✅ | START block |
| `step.operationId` + `parameters` (`name`/`in`/`value`) | ✅ | Numbered `01/02/03` step boxes |
| `step.requestBody` (`contentType`, `payload` with runtime exprs) | ✅ | Dark JSON block |
| `step.successCriteria` (`condition`) | ✅ | Yellow asserts block |
| `step.outputs` | ✅ | `→ name = $expr` (ordered, spec vocabulary preserved) |
| `step.onSuccess` (`end`, `goto`) and `step.onFailure` (`end`, `goto`, `retry`) | ✅ | ✅/❌ labelled sub-sections with action tag, retry meta (`× N`, `after Nms`), `when` criteria, and clickable anchor links to target steps |
| `step.onFailure: retry` targeting self | ✅ | Plus a CSS-drawn curved arrow on the right of the step (mobile fallback: banner above the step marker) |
| `workflow.outputs` | ✅ | END block |
| Qualified `operationId` (`$sourceDescriptions.<name>.<op>`) for multi-API workflows | ✅ | Linter enforces qualification when multiple sources are declared |
| `step.workflowId` (nested workflows) | ⏭️ | Future, would extend the rendering graph |
| `step.dependsOn` (parallel branches) | ⏭️ | Future |
| `components.parameters` / `components.{success,failure}Actions` (`$ref` reuse) | ⏭️ | Future |
| `Criterion.type` (`simple`/`regex`/`jsonpath`/`xpath`) + `Criterion.context` | ⚠️ | Read by the linter, not yet surfaced in the render |
| AsyncAPI sources | ❌ | Out of scope |

### 🌱 Eco-design and accessibility

These are **engineering constraints**, not afterthoughts. The rules are formalised in [`.agents/rules/`](./.agents/rules/) and enforced by reviews and tests:

- **Eco-design**: 1 network request at page load (Tailwind CDN), ~23 kB HTML, ~4 kB gzipped, no JavaScript, no fonts loaded from third parties, single Go binary (~5 MB) packaged in a `FROM scratch` Docker image.
- **Accessibility**: WCAG 2.2 AA contrasts (4.5:1 on body text), semantic HTML (`<main>`, `<section>`, `<h1>`→`<h2>`→`<h3>`), `aria-hidden` on decorative icons, visible focus, `prefers-reduced-motion` honoured, no info conveyed by colour alone, fluid `rem` sizing.

### 🧰 Built-in CLI

```text
arazzo-maestro --version                    Print version and exit
arazzo-maestro lint <file>                  Validate against schema + rules + cross-file
arazzo-maestro view <file> [flags]          Render to HTML

view flags:
  -o, --output <dir>          Output directory (default: dist)
      --workflow <id>         Only render this workflow
      --no-index              Skip generating index.html
      --theme <name>          Theme (default: light, or themes.yml's default:)
      --themes <path>         Path to a themes YAML (bypasses ./themes.yml)
      --list-themes           List available themes and exit
```

## Architecture

```
internal/
├── model/      Pure data types (no behaviour)
├── parser/     YAML → model.ArazzoDocument
├── linter/     Validates a document, three passes:
│               schema.go (official JSON Schema, via santhosh-tekuri/jsonschema)
│               linter.go (uniqueness, $steps.X.outputs.Y references)
│               crossfile.go (resolves sourceDescriptions[].url, checks operationIds)
├── theme/      Loads built-in + user themes, validates, audits WCAG contrast
└── renderer/   model + theme → standalone HTML (html/template + embedded assets)
cmd/arazzo-maestro/   Cobra CLI entry point
```

Dependency graph: `model` → ∅, `parser` → `model`, `linter` → `parser` + `model`, `theme` → ∅, `renderer` → `model` + `theme`, `cmd` → all. No cycles.

## What makes us different

There are already Arazzo plugins for VS Code and a Node-based validator from Jentic. They solve **authoring**: autocomplete, in-IDE preview, live validation while typing. We solve everything that happens **after** authoring:

| | Editor plugins | `arazzo-maestro` |
|---|---|---|
| Validate in CI / GitHub Actions / pre-commit | ❌ | ✅ |
| Share rendering with non-devs | ❌ Needs the IDE | ✅ Standalone HTML, any browser |
| Versionable artifact (commit, deploy to Pages) | ❌ Nothing to commit | ✅ HTML files |
| Zero runtime dependency | ❌ Needs IDE | ✅ Single Go binary, `FROM scratch` Docker |
| Cross-editor (vim, emacs, Zed, Cursor…) | ❌ Lock-in | ✅ Any editor or none |
| Explicit eco-design + accessibility contract | ❌ | ✅ Enforced by rules + tests |

These are complementary. The same user can have a VS Code plugin **and** `arazzo-maestro` in their CI.

## Examples

### CI workflow (GitHub Actions)

```yaml
# .github/workflows/arazzo.yml
name: arazzo
on: [pull_request, push]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }
      - run: |
          go install github.com/emmanuelperu/arazzo-maestro/cmd/arazzo-maestro@latest
          arazzo-maestro lint workflows/checkout.yaml

  publish:
    needs: lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: arazzo-maestro view workflows/checkout.yaml -o public/
      - uses: actions/upload-pages-artifact@v3
        with: { path: public/ }
```

### Pre-commit hook

```yaml
# .pre-commit-config.yaml
- repo: local
  hooks:
    - id: arazzo-lint
      name: arazzo-maestro lint
      entry: arazzo-maestro lint
      language: system
      files: '\.arazzo\.ya?ml$'
```

### Docker

The image at `ghcr.io/emmanuelperu/arazzo-maestro:latest` is published
once the first release is tagged (`v0.0.1`+). Until then, build it
locally from the in-repo [`Dockerfile`](./Dockerfile):

```bash
# Build locally
docker build --build-arg VERSION=0.0.1 \
  -t ghcr.io/emmanuelperu/arazzo-maestro:0.0.1 .

# Lint a file from the current directory
docker run --rm -v "$PWD":/work -w /work \
  ghcr.io/emmanuelperu/arazzo-maestro:0.0.1 \
  lint workflows/checkout.yaml

# Render to ./dist/ on the host
docker run --rm -v "$PWD":/work -w /work \
  ghcr.io/emmanuelperu/arazzo-maestro:0.0.1 \
  view workflows/checkout.yaml
```

`-v "$PWD":/work` exposes your current directory inside the container,
and `-w /work` makes it the working directory — so relative paths
(input file, `-o dist/` default) resolve where you expect on the host.
Without `-w`, the container's cwd is `/` and `view`'s default output
(`./dist/`) is written to `/dist/` inside the container, then discarded
by `--rm`.

The image is `FROM scratch` (~5 MB) — no shell, no libc, no package
manager. The binary is the entire userland.

## Roadmap

- [x] Bootstrap, parser, renderer, CLI
- [x] Theme system (light/dark + user `themes.yml` + WCAG audit)
- [x] Linter: JSON Schema + semantic rules + cross-file resolution
- [x] Visual identity: "Blueprint" (faint grid, navy + amber accent, schematic frames)
- [x] `onSuccess` / `onFailure` (incl. `retry` with `retryAfter` / `retryLimit`)
- [x] Retry-self CSS curved arrow, mobile fallback, anchor links on goto targets
- [x] `Makefile` + `examples/*.arazzo.yaml` convention (glob auto-pickup)
- [x] Step sub-section labels aligned to Arazzo spec vocabulary (`Parameters` / `Request Body` / `Success Criteria` / `Outputs`)
- [x] Second worked example: `checkout-branching.arazzo.yaml` with `goto` branching
- [x] OpenSSF Phase 1: CI (test + vet + golangci-lint + govulncheck), Scorecard, Dependabot, `SECURITY.md`, `CONTRIBUTING.md`
- [x] `Dockerfile` (`FROM scratch`, ~5 MB) with `VERSION` build-arg
- [x] OpenSSF Phase 2: `goreleaser` (multi-OS binaries + Docker image), cosign-signed releases, first tag `v0.0.1`
- [x] OpenSSF Phase 3: actions pinned to commit SHAs, CodeQL SAST workflow, `FuzzParseBytes` for the parser, `administration:read` so Scorecard's Branch-Protection check resolves
- [ ] Register on [bestpractices.dev](https://www.bestpractices.dev) (replaces the `OWNER` placeholder in the badge)
- [ ] Nested workflows (`step.workflowId`)
- [ ] `step.dependsOn` parallel branches
- [ ] `components.{parameters,successActions,failureActions}` `$ref` reuse
- [ ] `Criterion.type` + `context` surfaced in the render
- [ ] PNG export via headless Chromium
- [ ] Internalise Tailwind CSS (zero network requests at page load)
- [ ] HTTPS source URLs with deterministic caching (opt-in)
- [ ] Watch mode (`--watch`) using `fsnotify`

## Documentation

- [`AGENTS.md`](./AGENTS.md), entry point for any coding agent (humans too) working on this repo
- [`CONTRIBUTING.md`](./CONTRIBUTING.md), dev environment + PR checklist + conventions
- [`SECURITY.md`](./SECURITY.md), vulnerability reporting policy (private GitHub Security Advisories)
- [`Plan.md`](./Plan.md), full project history, decisions, roadmap, feature designs
- [`.agents/rules/`](./.agents/rules/), eco-design, accessibility, and code-style rules
- [`themes.yml.example`](./themes.yml.example), annotated theme template
- [`examples/`](./examples/), `*.arazzo.yaml` (Arazzo files, picked up by `make dist` / `make lint`) + their referenced OpenAPI contracts
- [`Makefile`](./Makefile), the canonical `make help|build|test|vet|lint|dist|clean` targets
- [`Dockerfile`](./Dockerfile), multi-stage `FROM scratch` build, accepts `--build-arg VERSION=…`

## Metrics

| Metric | Value |
|---|---|
| Generated HTML (`happy-path-checkout.html`) | ~23 kB raw, ~4 kB gzipped |
| Network requests at page load | 1 (Tailwind CDN, to be internalised) |
| Binary size (`-s -w -trimpath`) | ~5.1 MB |
| Docker image (`FROM scratch`) | ~5.0 MB |
| Direct dependencies | 3 (`cobra`, `yaml.v3`, `jsonschema`) |
| Lines of Go (excl. tests) | ~1,300 |
| Test coverage | parser 82 %, linter 83 %, theme 86 %, renderer 81 %, cmd 81 % |
| Built-in themes WCAG AA conformance | 100 % on critical pairs (11/11, incl. `jsonRuntime` on `jsonBg`) |

## Feedback

Found a bug, or have an idea to improve the tool? Your feedback is welcome:

- **Bug reports and feature requests**: [open an issue](https://github.com/emmanuelperu/arazzo-maestro/issues). Please describe what you expected, what happened, and the `arazzo-maestro` version where relevant.
- **Security vulnerabilities**: do not open a public issue. Follow the private disclosure process in [`SECURITY.md`](./SECURITY.md).

## Contributing

PRs welcome. See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for the full
guide (dev environment, PR checklist, conventions). The short version:

1. Read [`AGENTS.md`](./AGENTS.md) and the rules in [`.agents/rules/`](./.agents/rules/).
2. `make test vet` — both must be clean.
3. `make lint` — every `examples/*.arazzo.yaml` must lint with no issues.
4. `make dist` — every example must render without errors.
5. If you touch the HTML output, attach the gzipped byte count of an `examples/*.arazzo.yaml` rendering to the PR. Regressions > 10 % require discussion.

## Security

Please **do not** open public issues for security reports. See
[`SECURITY.md`](./SECURITY.md) for the supported channels and our
coordinated disclosure timeline.

## License

[Apache 2.0](./LICENSE). Compatible with enterprise legal review; patent grant included.

## Acknowledgements

- The [OpenAPI Initiative](https://www.openapis.org/) for the Arazzo specification.
- [`santhosh-tekuri/jsonschema`](https://github.com/santhosh-tekuri/jsonschema), the JSON Schema validator powering the linter's first pass.
- [Cobra](https://github.com/spf13/cobra), CLI framework.
- [`yaml.v3`](https://gopkg.in/yaml.v3), YAML parsing with node-level access.
- The [WebAIM](https://webaim.org/) contrast checker, the reference we test against.
