# Roadmap

## Done

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
- [x] `Dockerfile` (`FROM scratch`) with `VERSION` build-arg
- [x] OpenSSF Phase 2: `goreleaser` (multi-OS binaries + Docker image), cosign-signed releases, first tag `v0.0.1`
- [x] OpenSSF Phase 3: actions pinned to commit SHAs, CodeQL SAST workflow, `FuzzParseBytes` for the parser, `repo_token` plumbed for a `SCORECARD_TOKEN` PAT (unlocks Branch-Protection check once the secret is set)
- [x] OpenAPI source parsing via `pb33f/libopenapi` (`internal/oasresolver`, replaces the hand-rolled YAML walk)
- [x] `test gen e2e` / `test run e2e`: Hurl generation, runner with `--base-url`, `--variable`, optional HTML report
- [x] `test gen perf`: k6 script generation with `--vus` / `--duration` / `--threshold`, runtime expressions substituted in request bodies
- [x] Landscape layout (`view --layout landscape`), portrait stays the default
- [x] Mermaid export (`view --format mermaid`): one `.mmd` flowchart per workflow

## Planned

- [ ] Reach OpenSSF Best Practices `passing` badge ([project 12929](https://www.bestpractices.dev/projects/12929), currently `in_progress`)
- [ ] Shrink the binary (~5 MB before libopenapi, ~19 MB after: its `index` package links the whole `net/http`/TLS stack for remote `$ref` support we deliberately refuse)
- [ ] Nested workflows (`step.workflowId`)
- [ ] `step.dependsOn` parallel branches
- [ ] `components.{parameters,successActions,failureActions}` `$ref` reuse
- [ ] `Criterion.type` + `context` surfaced in the render
- [ ] SVG / PNG export (pure-Go layout pass, no headless browser)
- [ ] Internalise Tailwind CSS (zero network requests at page load)
- [ ] HTTPS source URLs with deterministic caching (opt-in)
- [ ] Watch mode (`--watch`) using `fsnotify`
