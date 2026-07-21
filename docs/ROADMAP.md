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
- [x] Spec-compliance wave (PRs #78 to #90): shared runtime-expression parser (`internal/expr`), requestBody `contentType` + `replacements` (`internal/payload`), `operationPath` resolution, honest degradation of `workflowId` steps, `components` reusable objects inlined at parse time, workflow-level `parameters`/`successActions`/`failureActions` defaults, `workflow.dependsOn` (parse + lint + render), criterion `context`/`type` badges, JSON-pointer sub-access on `$inputs`/`$steps` references

## Planned

- [ ] Reach OpenSSF Best Practices `passing` badge ([project 12929](https://www.bestpractices.dev/projects/12929), currently `in_progress`)
- [ ] Shrink the binary (~5 MB before libopenapi, ~20 MB after: its `index` package links the whole `net/http`/TLS stack for remote `$ref` support we deliberately refuse)
- [ ] Nested workflow execution (`step.workflowId` rendering + honest test-gen degradation shipped; expanding/inlining the invoked workflow is not)
- [ ] Parallel-branch rendering for `workflow.dependsOn` (the field is parsed, linted and displayed; the diagram does not lay branches out in parallel)
- [ ] SVG / PNG export (pure-Go layout pass, no headless browser)
- [ ] Internalise Tailwind CSS (zero network requests at page load)
- [ ] HTTPS source URLs with deterministic caching (opt-in)
- [ ] Watch mode (`--watch`) using `fsnotify`
