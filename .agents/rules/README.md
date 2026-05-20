# Project rules for agents

This directory holds the rules **every agent** (Claude Code, Cursor,
Copilot, Aider, …) must follow when coding in this project. Rules are
**non-negotiable** unless the maintainer explicitly overrides them,
with the rationale recorded in `Plan.md`.

Read these files **before** implementing anything touching HTML
output, templates, themes, or external dependencies.

## Index

| File | Scope |
|---|---|
| [`eco-design.md`](./eco-design.md) | Eco-design, minimise resources, requests, dependencies |
| [`accessibility.md`](./accessibility.md) | Accessibility, WCAG 2.2 AA target, HTML semantics, contrast |
| [`code-style.md`](./code-style.md) | Go conventions, language, comments |

## Priority

1. The rules in this directory
2. The project `Plan.md`
3. Implicit conventions of the existing code
4. The agent's global preferences

If a rule contradicts a user request, **flag the contradiction**
before acting, do not silently bypass a rule.
