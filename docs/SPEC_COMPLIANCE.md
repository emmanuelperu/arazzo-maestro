# Arazzo specification compliance

Honest, field-by-field status of `arazzo-maestro` against the official
[Arazzo 1.1.0 specification](https://spec.openapis.org/arazzo/latest.html),
audited section by section on 2026-06-05. Tracking issue:
[#58](https://github.com/emmanuelperu/arazzo-maestro/issues/58).

How to read the columns: **Parse** is the typed model the renderer and
the test generators consume; **Lint** distinguishes the embedded
official JSON Schema pass (1.0, version-patched) from the hand-rolled
semantic passes; **Render** is the `view` HTML; **TestGen** covers both
the Hurl and k6 generators. Verdicts:

- ✅ full: behaves as the spec requires
- 🟡 partial: the common cases work, documented limits remain
- 😶 dropped: accepted without error, then silently ignored downstream
- ❌ missing: not implemented
- ⛔ non-compliant: spec-valid input is rejected or corrupted

## Summary of known non-compliances

These actively reject or corrupt spec-valid documents, ranked by impact:

| Gap | Issue |
|---|---|
| `retryAfter` treated as integer milliseconds; spec says non-negative decimal **seconds** (decimals silently truncated; `retryLimit` default "single retry" not displayed) | [#41](https://github.com/emmanuelperu/arazzo-maestro/issues/41) |
| `in: cookie` parameters are silently omitted from generated tests | [#48](https://github.com/emmanuelperu/arazzo-maestro/issues/48) |
| `#/json-pointer` suffixes on `$inputs`/`$steps...outputs`, whole-body `$response.body`, and dotted names (legal per ABNF) are not translated by the generators | [#49](https://github.com/emmanuelperu/arazzo-maestro/issues/49) |

## Document-level objects

| Element | Parse | Lint | Render | TestGen | Verdict | Notes |
|---|---|---|---|---|---|---|
| `arazzo` version 1.0.x / 1.1.x | ✅ | ✅ | n/a | n/a | ✅ | Version pattern patched at schema load |
| `$self` (1.1) | ❌ | 😶 accepted | ❌ | ❌ | 😶 | Accepted structurally since #47; no base-URI / identity-based reference resolution |
| `info` | ✅ | ✅ | 🟡 | n/a | 🟡 | `description` and `version` parsed but never rendered |
| `sourceDescriptions` | ✅ | ✅ | ❌ | 🟡 | 🟡 | Never rendered; `type: arazzo` and `type: asyncapi` sources accepted but never resolved; HTTP/HTTPS URLs rejected by design (offline-first) |
| `components` + Reusable Objects | ❌ | 😶 schema-only | ❌ | ❌ | ❌ | [#52](https://github.com/emmanuelperu/arazzo-maestro/issues/52); a `{reference: $components...}` entry parses to an empty struct |
| `x-` extensions | 😶 | ✅ | ❌ | ❌ | 🟡 | Accepted, never surfaced |

## Workflow object

| Element | Parse | Lint | Render | TestGen | Verdict | Notes |
|---|---|---|---|---|---|---|
| `workflowId` | ✅ | ✅ uniqueness | ✅ | ✅ | ✅ | SHOULD-pattern not enforced (spec allows) |
| `summary` / `description` | ✅ | ✅ | ✅ | ✅ | ✅ | CommonMark rendered as plain text |
| `inputs` (JSON Schema 2020-12) | 🟡 | ✅ schema | 🟡 | 🟡 | 🟡 | Flattened to one level of `{type, default}`; `required`, nesting, enums dropped ([#57](https://github.com/emmanuelperu/arazzo-maestro/issues/57)) |
| `dependsOn` | ❌ | 😶 schema-only | ❌ | ❌ | ❌ | Targets never validated ([#50](https://github.com/emmanuelperu/arazzo-maestro/issues/50)) |
| `steps` | ✅ | ✅ | ✅ | ✅ | ✅ | |
| `successActions` / `failureActions` (workflow-level) | ❌ | 😶 schema-only | ❌ | ❌ | ❌ | Per-step override semantics unimplemented ([#50](https://github.com/emmanuelperu/arazzo-maestro/issues/50)) |
| `parameters` (workflow-level) | ❌ | 😶 schema-only | ❌ | ❌ | ❌ | Workflow-wide params never reach generated requests ([#50](https://github.com/emmanuelperu/arazzo-maestro/issues/50)) |
| `outputs` | ✅ | ✅ refs | ✅ | n/a | 🟡 | Selector Object form coerced to string |

## Step object and request body

| Element | Parse | Lint | Render | TestGen | Verdict | Notes |
|---|---|---|---|---|---|---|
| `stepId` | ✅ | ✅ uniqueness | ✅ | ✅ | ✅ | |
| `operationId` (short + qualified) | ✅ | ✅ cross-file | ✅ | ✅ | ✅ | |
| `operationPath` | ❌ | 😶 schema-only | ❌ | ❌ placeholder | ❌ | [#53](https://github.com/emmanuelperu/arazzo-maestro/issues/53) |
| `workflowId` (nested workflow) | ❌ | 😶 schema-only | ❌ "API" tag | ❌ misleading placeholder | ❌ | [#54](https://github.com/emmanuelperu/arazzo-maestro/issues/54) |
| `channelPath` (1.1, AsyncAPI) | ❌ | 😶 accepted | ❌ placeholder | ❌ | 😶 | Accepted structurally since #47; resolution out of scope (AsyncAPI) |
| `parameters` | ✅ | ✅ schema | ✅ | 🟡 | 🟡 | `cookie` and `querystring` dropped by generators ([#48](https://github.com/emmanuelperu/arazzo-maestro/issues/48)); Reusable entries parse empty ([#52](https://github.com/emmanuelperu/arazzo-maestro/issues/52)); `in` conditional rule unvalidated |
| `requestBody.contentType` / `payload` | ✅ | ✅ | ✅ | ✅ | ✅ | Whole-string and embedded `{$expr}` substitution |
| `requestBody.replacements` | ❌ | 😶 schema-only | ❌ | ❌ | ❌ | Generated bodies omit injected values ([#55](https://github.com/emmanuelperu/arazzo-maestro/issues/55)) |
| `successCriteria` | 🟡 | ✅ | 🟡 | 🟡 | 🟡 | Only `condition` survives the parser ([#51](https://github.com/emmanuelperu/arazzo-maestro/issues/51)) |
| `onSuccess` / `onFailure` | ✅ | ✅ targets + criteria | ✅ | n/a | 🟡 | Mutual exclusivity stepId/workflowId only schema-checked; generators do not emit retry/goto logic |
| `outputs` | ✅ | ✅ | ✅ | ✅ | ✅ | |

## Criteria and actions

| Element | Status | Notes |
|---|---|---|
| Criterion `condition` (simple grammar) | 🟡 | k6 translates the `$statusCode <op> <number>` subset to real `check()`s; everything else is an explicit comment in both generators (never guessed) |
| Criterion `context` / `type` (`simple`/`regex`/`jsonpath`/`xpath`) | 😶 | Schema enforces "context required with type"; dropped at the parser, indistinguishable downstream ([#51](https://github.com/emmanuelperu/arazzo-maestro/issues/51)) |
| Expression Type Object versions | ✅ schema | 1.1 values `rfc9535` and `jsonpointer`/`rfc6901` accepted since #47 (per the spec's Expression Type table, xpath versions stay 10/20/30); still dropped at the parser ([#51](https://github.com/emmanuelperu/arazzo-maestro/issues/51)) |
| `retryAfter` | ⛔ | Spec: decimal seconds; we store integer "milliseconds", truncate decimals, render "ms" ([#41](https://github.com/emmanuelperu/arazzo-maestro/issues/41)) |
| `retryLimit` | ⛔ | Absent means "a single retry" per spec; we display nothing, and `0` is indistinguishable from unset ([#41](https://github.com/emmanuelperu/arazzo-maestro/issues/41)) |

## Runtime expressions (ABNF)

| Form | Linter | Renderer | TestGen | Verdict |
|---|---|---|---|---|
| `$inputs.name`, `$steps.id.outputs.name` | ✅ existence + ordering | ✅ | ✅ | ✅ |
| `$inputs.name#/ptr`, `$steps...outputs.name#/ptr` | 🟡 suffix ignored | ✅ highlighted | ⛔ verbatim | ⛔ [#49](https://github.com/emmanuelperu/arazzo-maestro/issues/49) |
| `$response.body#/ptr` (captures) | n/a | ✅ | ✅ | ✅ |
| `$response.body` (whole body) | n/a | ✅ | ⛔ unsupported | ⛔ [#49](https://github.com/emmanuelperu/arazzo-maestro/issues/49) |
| `$statusCode` | n/a | ✅ | ✅ captures + k6 checks | ✅ |
| Embedded `{$expr}` in strings | n/a | ✅ | ✅ (declared `$inputs`/`$steps` only) | 🟡 |
| `$response.header/...`, `$request.*` (captures) | n/a | ✅ | 🟡 explicit `unsupported` marker | 🟡 |
| `$url`, `$method`, `$request.*`, `$message.*`, `$outputs.*`, `$workflows.*`, `$components.*`, `$self` (inline) | ❌ | 🟡 highlighted if `$`-prefixed | 😶 verbatim, no marker | 😶 [#56](https://github.com/emmanuelperu/arazzo-maestro/issues/56) |
| Names containing dots (legal per ABNF `identifier`) | ❌ | ✅ | ⛔ not translated | ⛔ [#49](https://github.com/emmanuelperu/arazzo-maestro/issues/49) |
| Expression grammar validation | ❌ | n/a | n/a | ❌ no ABNF validation pass exists |

## Out of scope by design

- AsyncAPI source resolution and message steps (the linter should still
  accept them structurally: [#47](https://github.com/emmanuelperu/arazzo-maestro/issues/47))
- Executing workflows (we generate tests; runners exist elsewhere)
- HTTP/HTTPS source URLs (offline-first eco-design rule; documented)

## Method

Six parallel reviews against the official spec text
(`OAI/Arazzo-Specification`, `versions/1.1.0.md`), one per area:
document objects, workflow object, step + request body, criteria +
actions, runtime-expression ABNF, parameters + documentation claims.
Every ⛔ above was verified against the embedded schema or by executing
the code, not inferred. Corrections welcome: file an issue with the
`spec-compliance` label.
