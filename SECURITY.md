# Security policy

## Supported versions

Until `arazzo-maestro` reaches `v1.0.0`, only the latest tagged
release receives security fixes. After `v1.0.0`, the policy will be
updated to cover the most recent minor release.

| Version | Status |
|---|---|
| latest tag | ✅ supported |
| `main`     | ⚠️ best-effort, no guarantees |
| older tags | ❌ not maintained |

## Reporting a vulnerability

**Please do not open a public GitHub issue for security reports.** A
public report gives potential attackers a head-start before a fix is
available.

Use the private channel instead:

→ **[Open a private GitHub Security Advisory](https://github.com/emmanuelperu/arazzo-maestro/security/advisories/new)** on this repository.

This creates a private discussion thread visible only to the
maintainers and the reporter. No email exchange is required, and
GitHub handles the workflow (acknowledgement, fix, CVE assignment,
public advisory) end-to-end.

When you report, please include:

- A clear description of the vulnerability and the affected version.
- Reproduction steps or a proof-of-concept (the smaller, the better).
- The impact you believe the vulnerability has (e.g. local DoS,
  arbitrary file read, etc.).
- Any suggested fix or mitigation.

## What to expect

| Step | Target |
|---|---|
| Acknowledgement of receipt | within **3 business days** |
| Initial assessment and severity rating | within **10 business days** |
| Coordinated disclosure timeline agreed | before any fix is published |
| Public advisory (CVE, if applicable) | after the fix lands |

We follow a coordinated disclosure model. We will work with you to
agree on a disclosure date before publishing any advisory.

## Scope

In scope:

- The Go code in this repository.
- The generated HTML output (e.g. XSS via injected runtime
  expressions).
- The embedded JSON Schema and theme files.
- The Docker image published from this repository.

Out of scope:

- Issues in Tailwind CSS (CDN dependency — report upstream).
- Issues in third-party Go modules — please report directly to the
  module maintainer. We will pick up the fix via `go mod tidy` once a
  patched version is available.
- Social engineering, physical access, or denial of service requiring
  unrealistic computational resources.

## Security-related practices

The project applies the following defensive practices, audited by CI
on every pull request:

- `govulncheck` runs against every change and the latest stable Go
  release.
- `golangci-lint` (with `gosec`, `errcheck`, `errorlint`, etc.) gates
  merges.
- All HTML output is auto-escaped through `html/template`, with
  explicit `html.EscapeString` calls on any string injected into
  raw-HTML constructors (`template.HTML`, `template.CSS`).
- The Go binary is built with no cgo and reproducible flags. Release
  artefacts will be signed via Sigstore once `goreleaser` is enabled
  (see [`Plan.md`](./Plan.md) > OpenSSF Phase 2).
- Cross-file resolution refuses HTTP/HTTPS URLs in `sourceDescriptions[].url`
  — the linter only reads from local disk to keep the supply chain
  predictable.

## Acknowledgements

We thank all responsible disclosure contributors. With your
permission, we will credit you in the advisory and the release notes.
