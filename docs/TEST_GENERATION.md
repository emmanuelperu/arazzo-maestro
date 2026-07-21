# Test generation

Turn an Arazzo workflow into a runnable test artifact, then run it against any environment. The subcommand grammar reflects what kind of test you want, and only then the target technology:

```text
arazzo-maestro test gen e2e  <file> [flags]      Write end-to-end functional tests to disk
arazzo-maestro test run e2e  <file> [flags]      Generate + run them against an endpoint
arazzo-maestro test gen perf <file> [flags]      Load / performance tests (k6)
```

The kind (`e2e` / `perf`) is a subcommand so each one declares its own flags: e2e doesn't pretend to know about virtual users, perf doesn't pretend to know about response assertions. The target technology is picked through `--format`, with a sensible default per kind. Cobra validates the combination, so `--format=drill` on `e2e` is rejected at parse time; no manual validation code.

## e2e: `--format=hurl` (default)

Each workflow produces one `.hurl` file under the output directory, organised by kind / format / Arazzo source:

```bash
arazzo-maestro test gen e2e examples/shop.arazzo.yaml -o dist/
# → wrote dist/e2e/hurl/shop/happy-path-checkout.hurl
# → wrote dist/e2e/hurl/shop/payment-refused-path.hurl
```

The `e2e/<format>/<arazzo-name>/` prefix is added by the CLI so a single output directory can hold artifacts for several Arazzo files and several kinds (e2e, perf, ...) without collisions, and so the on-disk layout mirrors the subcommand grammar.

The generator delegates `operationId` and `operationPath` resolution to the OpenAPI sources declared under `sourceDescriptions` (loaded as local files only; HTTP/HTTPS URLs are rejected, same eco-design rule as the linter). Resolved steps emit real `METHOD {{baseUrl}}/path` request lines with path parameters substituted; unresolvable steps emit a Hurl comment and a placeholder so the file stays valid for the target tool. Steps that invoke another workflow (`workflowId`) emit an explicit not-supported comment and no request at all, and references to their outputs stay visibly untranslated (no capture would ever define them).

The request host is never hard-coded. Every request line is prefixed with the `{{baseUrl}}` Hurl variable, so the **same** `.hurl` file runs unchanged against staging, pre-production or a local mock by passing the endpoint at run time. The OpenAPI `servers:` URL, when present, is surfaced in the file header as the documented default.

Arazzo step features translated:

| Arazzo                     | Hurl                                          |
|----------------------------|-----------------------------------------------|
| request host               | `{{baseUrl}}` variable (set per environment)  |
| `parameters` in=header     | header lines on the request                   |
| `parameters` in=query      | `[QueryStringParams]` block                   |
| `parameters` in=path       | substituted into the URL template             |
| `parameters` in=cookie     | `[Cookies]` block                             |
| `parameters` in=querystring | appended to the request URL                  |
| `step.outputs`             | `[Captures]` with `jsonpath` / `status`       |
| `step.successCriteria`     | comments inside `[Asserts]`                   |
| `requestBody.contentType`  | `Content-Type:` header (falls back to the operation's declared type when omitted) |
| `requestBody.replacements` | applied to the payload (RFC 6901 target) before substitution, echoed as comments |
| `$inputs.foo`              | `{{foo}}` (Hurl variable)                     |
| `{$inputs.foo}` embedded in text | `{{foo}}` inside the string             |
| `$steps.s.outputs.o`       | `{{s_o}}` (capture-chained)                   |
| `$steps.s.outputs.o#/x/0`  | `{{s_o_x_0}}` (derived capture at step `s`, pointer folded into the source jsonpath) |
| `$response.body#/x/y`      | `jsonpath "$.x.y"`                            |
| `$statusCode`              | `status`                                      |

`$inputs.foo#/x` stays flagged as untranslated in Hurl: a variable
cannot be sub-accessed at render time (member access on a structured
value is unrenderable and placeholder filters are silently ignored,
verified against hurl 8.0.1). The k6 generator translates it through a
per-reference `asJson()` parse instead.

## Running against your environment: `test run e2e`

It generates the tests on the fly and executes them with [Hurl](https://hurl.dev) against the endpoint you pass, optionally writing Hurl's HTML report. You choose the target at run time, so the same workflow validates staging, then pre-prod, then prod. No environment at hand? `./mocking/start-mock-server.sh` boots a local [Microcks](https://microcks.io) instance, imports the example contracts and prints the exact commands to run the e2e and perf suites against the mocks (see [`mocking/README.md`](../mocking/README.md)):

```bash
# Run against a pre-production endpoint
arazzo-maestro test run e2e examples/shop.arazzo.yaml \
  --base-url https://staging.shop.example.com/api/v1 \
  --variable productId=p-001 --variable orderId=ord-1 --variable acceptLanguage=en

# Same thing, plus an HTML report (Hurl's native format)
arazzo-maestro test run e2e examples/shop.arazzo.yaml \
  --base-url https://staging.shop.example.com/api/v1 \
  --report-html dist/hurl-report \
  --variable productId=p-001 --variable orderId=ord-1 --variable acceptLanguage=en
# → open dist/hurl-report/index.html
```

`--base-url` sets the `{{baseUrl}}` variable; `--variable name=value` (repeatable) supplies the workflow inputs listed in each generated file's header. The process exit status mirrors Hurl (non-zero on any failure), and with `--report-html` the report is written even when tests fail, so CI can publish it as an artifact. Hurl must be on `PATH` ([install](https://hurl.dev/docs/installation.html), e.g. `brew install hurl`); prefer `test gen e2e` when you only want the files.

## perf: `--format=k6`

Each workflow becomes one k6 script (issue #22):

```bash
arazzo-maestro test gen perf shop.arazzo.yaml -o dist/ \
  --vus=10 --duration=30s \
  --threshold='http_req_duration=p(95)<500' --threshold='http_req_failed=rate<0.01'
# → wrote dist/perf/k6/shop/happy-path-checkout.k6.js
```

The load profile and thresholds are not part of Arazzo, so they come from flags and land in the script's exported `options`. Each `--threshold` is a k6 `metric=expression` (repeatable); a bare `expression` defaults the metric to `http_req_duration`. The generated script reads its target from the `BASE_URL` environment variable (default: the OpenAPI `servers:` URL) and each workflow input from a same-named variable, so the same script runs anywhere:

```bash
k6 run -e BASE_URL=https://staging.example.com -e productId=p-001 \
  dist/perf/k6/shop/happy-path-checkout.k6.js
```

Workflow steps become `http.request(...)` calls, outputs become captures (`res.json(...)`, `res.status`) chained into later steps, and status-code success criteria become `check()` predicates (other conditions are emitted as comments rather than guessed at). Runtime expressions inside a request body are substituted too (`"$inputs.productId"` becomes the `productId` constant; the e2e generator emits `{{productId}}`, unquoted when the input's declared type is numeric or boolean), as is the spec's embedded form: `"Bearer {$inputs.token}"` becomes a JS template literal in k6 and `"Bearer {{token}}"` in Hurl. Payload `replacements` are applied (JSON-pointer target) before substitution, in both generators. Only whole-string expressions and braced `{$expr}` occurrences resolving to a declared input or earlier step output are substituted; anything else stays a literal. k6 constants are assigned at declaration time in one table: when two names sanitise to the same JS identifier (`user.name` vs `user_name`), the later one gets a numeric suffix (`user_name_2`) so each input keeps its own environment variable, and references translate by exact Arazzo name, never by identifier coincidence. Drill is a planned lighter alternative.

The perf-only flags (`--vus`, `--duration`, `--threshold`) live on `test gen perf` so `test gen perf --help` documents exactly what makes sense for load testing; `test gen e2e --help` stays focused on functional concerns. Both subcommands share the same underlying workflow IR, so adding a new format is a per-template change, not a CLI redesign.

## Where does test data come from?

Three layers, demonstrated by [`auth.arazzo.yaml`](../examples/auth.arazzo.yaml):

1. **Run-time inputs.** Workflow `inputs` become Hurl variables and k6 environment variables; secrets never live in the YAML, they come from your shell or CI secret store:

   ```bash
   arazzo-maestro test run e2e examples/auth.arazzo.yaml --base-url https://staging.example.com \
     --variable username=demo --variable password="$ORDERS_PASSWORD"
   k6 run -e BASE_URL=https://staging.example.com -e username=demo -e password="$ORDERS_PASSWORD" \
     dist/perf/k6/auth/authenticated-order-lookup.k6.js
   ```

2. **Captured outputs.** Authentication is just a step: the login response token is captured and chained into later headers (`Bearer {$steps.login.outputs.token}` becomes `Bearer {{login_token}}` in Hurl and a JS template literal in k6).
3. **Self-provisioned data.** Values that must exist server-side are derived from earlier responses (the order id comes from the listing step), so the scenario carries its own fixtures to any environment.
