package k6gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
)

const shopSpec = `
openapi: "3.1.0"
info: { title: Shop, version: "1.0.0" }
servers:
  - url: https://api.shop.test
paths:
  /products:
    get:
      operationId: listProducts
      responses: { "200": { description: ok } }
  /products/{id}:
    get:
      operationId: getProduct
      responses: { "200": { description: ok } }
  /orders:
    post:
      operationId: createOrder
      responses: { "201": { description: created } }
`

const noServerSpec = `
openapi: "3.1.0"
info: { title: NoServer, version: "1.0.0" }
paths:
  /ping:
    get:
      operationId: ping
      responses: { "200": { description: ok } }
`

func loadSource(t *testing.T, spec string) *oasresolver.Source {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(path, []byte(spec), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	src, err := oasresolver.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return src
}

func shopSources(t *testing.T) map[string]*oasresolver.Source {
	t.Helper()
	return map[string]*oasresolver.Source{"shop": loadSource(t, shopSpec)}
}

func gen(t *testing.T, wf model.Workflow, sources map[string]*oasresolver.Source, opts Options) string {
	t.Helper()
	out, err := Generate(wf, sources, opts)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return out
}

func defaultOpts() Options {
	return Options{VUs: 1, Duration: "30s"}
}

func assertContains(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Errorf("output missing %q\n--- output ---\n%s\n--- end ---", want, out)
	}
}

func assertNotContains(t *testing.T, out, want string) {
	t.Helper()
	if strings.Contains(out, want) {
		t.Errorf("output contains unwanted %q\n--- output ---\n%s\n--- end ---", want, out)
	}
}

func TestGenerateEmitsHeaderImportsAndDefaultFunc(t *testing.T) {
	wf := model.Workflow{WorkflowID: "empty", Summary: "Nothing here"}
	out := gen(t, wf, nil, defaultOpts())
	assertContains(t, out, "// Workflow: empty")
	assertContains(t, out, "// Nothing here")
	assertContains(t, out, "import http from 'k6/http';")
	assertContains(t, out, "import { check } from 'k6';")
	assertContains(t, out, "export default function () {")
	assertNotContains(t, out, "// Step:")
}

func TestGenerateBaseURLReadsEnvWithServersDefault(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps:      []model.Step{{StepID: "list", OperationID: "listProducts"}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, `const BASE_URL = __ENV.BASE_URL || "https://api.shop.test";`)
	// The default is documented in the header too.
	assertContains(t, out, "default (OpenAPI servers): https://api.shop.test")
	// The request stays environment-agnostic.
	assertContains(t, out, "${BASE_URL}/products")
	assertNotContains(t, out, "https://api.shop.test/products")
}

func TestGenerateBaseURLEmptyWhenNoServer(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps:      []model.Step{{StepID: "ping", OperationID: "ping"}},
	}
	out := gen(t, wf, map[string]*oasresolver.Source{"x": loadSource(t, noServerSpec)}, defaultOpts())
	assertContains(t, out, `const BASE_URL = __ENV.BASE_URL || "";`)
	assertContains(t, out, "${BASE_URL}/ping")
}

func TestGenerateWritesLoadProfile(t *testing.T) {
	wf := model.Workflow{WorkflowID: "wf"}
	out := gen(t, wf, nil, Options{VUs: 10, Duration: "30s"})
	assertContains(t, out, "export const options = {")
	assertContains(t, out, "vus: 10,")
	assertContains(t, out, `duration: "30s",`)
	// No thresholds key when none are supplied.
	assertNotContains(t, out, "thresholds:")
}

func TestGenerateWritesThresholdsDeterministically(t *testing.T) {
	wf := model.Workflow{WorkflowID: "wf"}
	out := gen(t, wf, nil, Options{
		VUs:      5,
		Duration: "1m",
		Thresholds: map[string][]string{
			"http_req_failed":   {"rate<0.01"},
			"http_req_duration": {"p(95)<500", "p(99)<800"},
		},
	})
	assertContains(t, out, "thresholds: {")
	assertContains(t, out, `"http_req_duration": ["p(95)<500", "p(99)<800"],`)
	assertContains(t, out, `"http_req_failed": ["rate<0.01"],`)
	// Sorted: duration before failed.
	if i, j := strings.Index(out, "http_req_duration"), strings.Index(out, "http_req_failed"); i > j {
		t.Errorf("thresholds not sorted: duration at %d, failed at %d", i, j)
	}
}

func TestGenerateResolvesShortFormOperationID(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps:      []model.Step{{StepID: "list", OperationID: "listProducts"}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "const listRes = http.request('GET', `${BASE_URL}/products`, null, { headers: {} });")
	assertNotContains(t, out, "__unresolved__")
}

func TestGenerateResolvesQualifiedOperationID(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps:      []model.Step{{StepID: "list", OperationID: "$sourceDescriptions.shop.listProducts"}},
	}
	out := gen(t, wf, map[string]*oasresolver.Source{
		"shop":  loadSource(t, shopSpec),
		"other": loadSource(t, noServerSpec),
	}, defaultOpts())
	assertContains(t, out, "http.request('GET', `${BASE_URL}/products`")
	assertNotContains(t, out, "__unresolved__")
}

func TestGenerateFallsBackToPlaceholderWhenUnresolved(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps:      []model.Step{{StepID: "ghost", OperationID: "noSuchOp"}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "// unresolved operationId: noSuchOp")
	assertContains(t, out, "${BASE_URL}/__unresolved__/noSuchOp")
}

func TestGenerateAcceptsNilSourcesMap(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps:      []model.Step{{StepID: "x", OperationID: "anything"}},
	}
	out := gen(t, wf, nil, defaultOpts())
	assertContains(t, out, "// unresolved operationId: anything")
}

func TestGenerateSubstitutesPathParameters(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "get",
			OperationID: "getProduct",
			Parameters:  []model.Parameter{{Name: "id", In: "path", Value: "p-001"}},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "${BASE_URL}/products/p-001")
	assertNotContains(t, out, "{id}")
}

func TestGenerateSubstitutesPathParameterFromRuntimeExpression(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Inputs:     []model.InputProperty{{Name: "productId", Type: "string"}},
		Steps: []model.Step{{
			StepID:      "get",
			OperationID: "getProduct",
			Parameters:  []model.Parameter{{Name: "id", In: "path", Value: "$inputs.productId"}},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "${BASE_URL}/products/${productId}")
}

func TestGenerateEmitsQueryAndHeaderParameters(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			Parameters: []model.Parameter{
				{Name: "Authorization", In: "header", Value: "Bearer x"},
				{Name: "limit", In: "query", Value: 10},
				{Name: "cursor", In: "query", Value: "$steps.prev.outputs.next"},
			},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "${BASE_URL}/products?limit=10&cursor=${prev_next}")
	assertContains(t, out, `headers: { "Authorization": "Bearer x" }`)
}

func TestGenerateEmitsStringRequestBody(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "create",
			OperationID: "createOrder",
			RequestBody: &model.RequestBody{ContentType: "application/json", Payload: "raw-payload"},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "// requestBody content-type: application/json")
	assertContains(t, out, `const createBody = "raw-payload";`)
	assertContains(t, out, "http.request('POST', `${BASE_URL}/orders`, createBody,")
}

func TestGenerateMarshalsMapBodyAsJSONAndStringifies(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "create",
			OperationID: "createOrder",
			RequestBody: &model.RequestBody{
				ContentType: "application/json",
				Payload:     map[string]any{"currency": "EUR"},
			},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, `"currency": "EUR"`)
	assertContains(t, out, "http.request('POST', `${BASE_URL}/orders`, JSON.stringify(createBody),")
	assertNotContains(t, out, "map[")
}

func TestGenerateTranslatesExprsInsideJSONBody(t *testing.T) {
	tests := []struct {
		name    string
		payload any
		want    []string
		notWant []string
	}{
		{
			name:    "flat object value",
			payload: map[string]any{"productId": "$inputs.productId", "quantity": 2},
			want:    []string{`"productId": productId,`, `"quantity": 2`},
			notWant: []string{`"$inputs.productId"`},
		},
		{
			name:    "step output reference is sanitised",
			payload: map[string]any{"cartId": "$steps.add-to-cart.outputs.cartId"},
			want:    []string{`"cartId": add_to_cart_cartId`},
		},
		{
			name:    "nested object",
			payload: map[string]any{"customer": map[string]any{"id": "$inputs.customerId"}},
			want:    []string{`"id": customerId`},
		},
		{
			name: "nested array",
			payload: map[string]any{"items": []any{
				"$inputs.productId",
				map[string]any{"ref": "$steps.list.outputs.first"},
			}},
			want: []string{"      productId,", `"ref": list_first`},
		},
		{
			name:    "unrecognised expression form stays a literal string",
			payload: map[string]any{"note": "$response.body#/x"},
			want:    []string{`"note": "$response.body#/x"`},
		},
		{
			name:    "raw string payload that is an expression",
			payload: "$inputs.rawBody",
			want:    []string{"const createBody = rawBody;"},
			notWant: []string{`"$inputs.rawBody"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := model.Workflow{
				WorkflowID: "wf",
				Inputs: []model.InputProperty{
					{Name: "productId", Type: "string"},
					{Name: "customerId", Type: "string"},
					{Name: "rawBody", Type: "string"},
				},
				Steps: []model.Step{
					{
						StepID:      "add-to-cart",
						OperationID: "createOrder",
						Outputs:     []model.OutputEntry{{Name: "cartId", Expression: "$response.body#/cartId"}},
					},
					{
						StepID:      "list",
						OperationID: "listProducts",
						Outputs:     []model.OutputEntry{{Name: "first", Expression: "$response.body#/items/0/id"}},
					},
					{
						StepID:      "create",
						OperationID: "createOrder",
						RequestBody: &model.RequestBody{ContentType: "application/json", Payload: tt.payload},
					},
				},
			}
			out := gen(t, wf, shopSources(t), defaultOpts())
			for _, w := range tt.want {
				assertContains(t, out, w)
			}
			for _, nw := range tt.notWant {
				assertNotContains(t, out, nw)
			}
		})
	}
}

func TestGenerateLeavesUndeclaredOrMalformedBodyExprsAsLiterals(t *testing.T) {
	tests := []struct {
		name    string
		payload any
		want    []string
		notWant []string
	}{
		{
			name:    "free text after the inputs prefix",
			payload: map[string]any{"note": "$inputs.tax is included"},
			want:    []string{`"note": "$inputs.tax is included"`},
			notWant: []string{"tax_is_included"},
		},
		{
			name:    "dotted input sub-path",
			payload: map[string]any{"name": "$inputs.user.name"},
			want:    []string{`"name": "$inputs.user.name"`},
			notWant: []string{"user_name"},
		},
		{
			name:    "undeclared input",
			payload: map[string]any{"id": "$inputs.ghost"},
			want:    []string{`"id": "$inputs.ghost"`},
		},
		{
			name:    "forward step reference",
			payload: map[string]any{"cartId": "$steps.later.outputs.cartId"},
			want:    []string{`"cartId": "$steps.later.outputs.cartId"`},
			notWant: []string{`"cartId": later_cartId`},
		},
		{
			name:    "raw string payload referencing an undeclared input",
			payload: "$inputs.ghost",
			want:    []string{`const createBody = "$inputs.ghost";`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := model.Workflow{
				WorkflowID: "wf",
				Inputs:     []model.InputProperty{{Name: "user", Type: "object"}},
				Steps: []model.Step{
					{
						StepID:      "create",
						OperationID: "createOrder",
						RequestBody: &model.RequestBody{ContentType: "application/json", Payload: tt.payload},
					},
					{
						StepID:      "later",
						OperationID: "listProducts",
						Outputs:     []model.OutputEntry{{Name: "cartId", Expression: "$response.body#/cartId"}},
					},
				},
			}
			out := gen(t, wf, shopSources(t), defaultOpts())
			for _, w := range tt.want {
				assertContains(t, out, w)
			}
			for _, nw := range tt.notWant {
				assertNotContains(t, out, nw)
			}
		})
	}
}

func TestGenerateTranslatesStatusCodeCriteriaToChecks(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			SuccessCriteria: []model.SuccessCriterion{
				{Condition: "$statusCode == 200"},
				{Condition: "$statusCode != 500"},
				{Condition: "$response.body#/items != []"},
			},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "check(listRes, {")
	assertContains(t, out, `"list: $statusCode == 200": (r) => r.status === 200,`)
	assertContains(t, out, `"list: $statusCode != 500": (r) => r.status !== 500,`)
	// The non-status condition is not guessed at, only commented.
	assertContains(t, out, "// successCriteria (not translated): $response.body#/items != []")
}

func TestGenerateEmitsCapturesNamespacedByStep(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			Outputs: []model.OutputEntry{
				{Name: "first", Expression: "$response.body#/items/0/id"},
				{Name: "code", Expression: "$statusCode"},
				{Name: "weird", Expression: "$unknown.thing"},
			},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, `const list_first = listRes.json("items.0.id");`)
	assertContains(t, out, "const list_code = listRes.status;")
	assertContains(t, out, "const list_weird = null; // unsupported: $unknown.thing")
}

func TestGenerateChainsStepCaptureToLaterReference(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "auth",
				OperationID: "listProducts",
				Outputs:     []model.OutputEntry{{Name: "token", Expression: "$response.body#/token"}},
			},
			{
				StepID:      "use",
				OperationID: "createOrder",
				Parameters:  []model.Parameter{{Name: "Authorization", In: "header", Value: "$steps.auth.outputs.token"}},
			},
		},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, `const auth_token = authRes.json("token");`)
	assertContains(t, out, `headers: { "Authorization": auth_token }`)
}

func TestGenerateSanitisesHyphenatedIdentifiers(t *testing.T) {
	// Arazzo step ids and input names allow hyphens, which are not valid
	// JS identifiers; the capture declaration and any later reference must
	// be sanitised to the SAME name or the script throws ReferenceError.
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "list-catalog",
				OperationID: "listProducts",
				Outputs:     []model.OutputEntry{{Name: "next-cursor", Expression: "$response.body#/next"}},
			},
			{
				StepID:      "use",
				OperationID: "listProducts",
				Parameters:  []model.Parameter{{Name: "cursor", In: "query", Value: "$steps.list-catalog.outputs.next-cursor"}},
			},
		},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "const list_catalogRes = http.request(")
	assertContains(t, out, `const list_catalog_next_cursor = list_catalogRes.json("next");`)
	assertContains(t, out, "cursor=${list_catalog_next_cursor}")
}

func TestGenerateDeclaresInputsFromEnvWithDefault(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Inputs: []model.InputProperty{
			{Name: "productId", Type: "string", Default: "p-001"},
			{Name: "qty", Type: "integer"},
		},
	}
	out := gen(t, wf, nil, defaultOpts())
	assertContains(t, out, `const productId = __ENV["productId"] || "p-001";`)
	assertContains(t, out, `const qty = __ENV["qty"] || '';`)
}

func TestGenerateSeparatesMultipleSteps(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "first", OperationID: "listProducts"},
			{StepID: "second", OperationID: "createOrder"},
		},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "// Step: first")
	assertContains(t, out, "// Step: second")
}
