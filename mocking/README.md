# Local mock server (Microcks)

Boots a local [Microcks](https://microcks.io) instance and imports the
OpenAPI contracts from [`examples/`](../examples), so the generated e2e
(Hurl) and perf (k6) tests run against real HTTP mocks instead of the
in-memory demo mock (`go run ./scripts/hurl-demo`, shop only).

```bash
./mocking/start-mock-server.sh   # up + import + dispatchers, prints ready-to-copy commands
./mocking/stop-mock-server.sh    # tear down
```

Requirements: Docker (with the `docker compose` plugin), `curl`,
`python3`. The Microcks UI is served at <http://localhost:8082>.

## How the mocks are driven

Microcks builds its mock dataset from the **named `examples:`** of the
OpenAPI contract, never from the singular `example:` field:

- A **response example** is what the mock returns. An operation without
  a named response example answers `400 The response does not exist!`.
- **Path and query parameter examples** with the *same name* become the
  dispatch criteria: `GET /products/p-001` matches the `success`
  example only because the `productId` parameter declares
  `examples.success.value: p-001`. A request whose parameters match no
  example's criteria answers `404`.
- Example values must line up with what the generated tests send, and
  chained captures must resolve: the `items[0].id` returned by the
  catalog example is the id the next step fetches.

Every operation in the example contracts carries one canonical
`success` exchange following these rules.

## Body-based dispatching

Routing on the **request body** cannot be expressed in an OpenAPI
contract; it is a Microcks operation setting pushed by the start script
after the import (a `JSON_BODY` dispatcher). One is configured for the
Shop API payment operation: the card ending `1111` returns the
`success` example (`status: OK`), the card ending `0002` returns
`refused` (`status: REFUSED`).

## Troubleshooting

Probe a mock URL with `curl` before blaming the tests:

- `400 The response does not exist!`: the operation has no named
  response example (or the dispatcher points at a missing name).
- `404`: dispatch criteria mismatch, the request's path/query values do
  not match any parameter example of the same name.
