package k6gen

import (
	"math"
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
		Steps: []model.Step{
			{
				StepID:      "prev",
				OperationID: "createOrder",
				Outputs:     []model.OutputEntry{{Name: "next", Expression: "$response.body#/next"}},
			},
			{
				StepID:      "list",
				OperationID: "listProducts",
				Parameters: []model.Parameter{
					{Name: "Authorization", In: "header", Value: "Bearer x"},
					{Name: "limit", In: "query", Value: 10},
					{Name: "cursor", In: "query", Value: "$steps.prev.outputs.next"},
				},
			},
		},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "${BASE_URL}/products?limit=10&cursor=${prev_next}")
	assertContains(t, out, `headers: { "Authorization": "Bearer x" }`)
}

func TestGenerateTranslatesEmbeddedExprInParameters(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Inputs:     []model.InputProperty{{Name: "token", Type: "string"}, {Name: "version", Type: "string"}},
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			Parameters: []model.Parameter{
				{Name: "Authorization", In: "header", Value: "Bearer {$inputs.token}"},
				{Name: "v", In: "query", Value: "api-{$inputs.version}"},
			},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "\"Authorization\": `Bearer ${token}`")
	assertContains(t, out, "v=api-${version}")
}

func TestGenerateLeavesUndeclaredParamExprsAsLiterals(t *testing.T) {
	// Same rule as bodies: a parameter expression referencing nothing
	// declared must not become an undefined JS identifier.
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			Parameters: []model.Parameter{
				{Name: "cursor", In: "query", Value: "$steps.ghost.outputs.next"},
				{Name: "X-Auth", In: "header", Value: "$inputs.ghost"},
			},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "cursor=$steps.ghost.outputs.next")
	assertContains(t, out, `"X-Auth": "$inputs.ghost"`)
}

func TestGenerateLeavesUndeclaredEmbeddedURLExprAsLiteral(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			Parameters: []model.Parameter{
				{Name: "v", In: "query", Value: "api-{$inputs.ghost}"},
			},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "v=api-{$inputs.ghost}")
}

func TestGenerateEscapesURLParamTextInsideTemplateLiteral(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Inputs:     []model.InputProperty{{Name: "version", Type: "string"}},
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			Parameters: []model.Parameter{
				{Name: "v", In: "query", Value: "a`b-{$inputs.version}"},
			},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "v=a\\`b-${version}")
}

func TestGenerateEmitsCookieAndQuerystringParameters(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Inputs:     []model.InputProperty{{Name: "token", Type: "string"}, {Name: "qs", Type: "string"}},
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			Parameters: []model.Parameter{
				{Name: "session", In: "cookie", Value: "$inputs.token"},
				{Name: "theme", In: "cookie", Value: "dark"},
				{Name: "q", In: "querystring", Value: "$inputs.qs"},
			},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "${BASE_URL}/products?${qs}")
	assertContains(t, out, `cookies: { "session": token, "theme": "dark" }`)
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
		{
			name:    "empty name after the inputs prefix",
			payload: map[string]any{"id": "$inputs."},
			want:    []string{`"id": "$inputs."`},
		},
		{
			name:    "step reference without an outputs segment",
			payload: map[string]any{"id": "$steps.foo.bar"},
			want:    []string{`"id": "$steps.foo.bar"`},
		},
		{
			name:    "dotted step output sub-path",
			payload: map[string]any{"ref": "$steps.later.outputs.a.b"},
			want:    []string{`"ref": "$steps.later.outputs.a.b"`},
		},
		{
			name:    "expression embedded in surrounding text",
			payload: map[string]any{"auth": "Bearer $inputs.user"},
			want:    []string{`"auth": "Bearer $inputs.user"`},
		},
		{
			name:    "literal sentinel-looking string does not steal the swap",
			payload: map[string]any{"a": "__arazzo_expr_0__", "z": "$inputs.user"},
			want:    []string{`"a": "__arazzo_expr_0__"`, `"z": user`},
		},
		{
			name:    "embedded braced expression becomes a template literal",
			payload: map[string]any{"auth": "Bearer {$inputs.user}"},
			want:    []string{"\"auth\": `Bearer ${user}`"},
		},
		{
			name:    "undeclared braced expression stays a literal",
			payload: map[string]any{"auth": "Bearer {$inputs.ghost}"},
			want:    []string{`"auth": "Bearer {$inputs.ghost}"`},
		},
		{
			name:    "template literal syntax in surrounding text is escaped",
			payload: map[string]any{"v": "a`b${c} {$inputs.user}"},
			want:    []string{"\"v\": `a\\`b\\${c} ${user}`"},
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

func TestGenerateRendersStepDescription(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			Description: "Browse the catalog\nfirst page only",
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "  // Browse the catalog\n  // first page only\n")
}

func TestGenerateBodyEdgeCases(t *testing.T) {
	bodyWf := func(payload any) model.Workflow {
		return model.Workflow{
			WorkflowID: "wf",
			Steps: []model.Step{{
				StepID:      "create",
				OperationID: "createOrder",
				RequestBody: &model.RequestBody{ContentType: "application/json", Payload: payload},
			}},
		}
	}
	t.Run("empty containers render as {} and []", func(t *testing.T) {
		out := gen(t, bodyWf(map[string]any{"obj": map[string]any{}, "arr": []any{}}), shopSources(t), defaultOpts())
		assertContains(t, out, `"obj": {}`)
		assertContains(t, out, `"arr": []`)
	})
	t.Run("unmarshalable map value falls back to a quoted literal", func(t *testing.T) {
		out := gen(t, bodyWf(map[string]any{"bad": math.NaN()}), shopSources(t), defaultOpts())
		assertContains(t, out, `const createBody = "map[bad:NaN]";`)
		assertNotContains(t, out, "JSON.stringify")
	})
	t.Run("unmarshalable array value falls back to a quoted literal", func(t *testing.T) {
		out := gen(t, bodyWf([]any{math.NaN()}), shopSources(t), defaultOpts())
		assertContains(t, out, `const createBody = "[NaN]";`)
		assertNotContains(t, out, "JSON.stringify")
	})
}

func TestGenerateSanitisesLeadingDigitIdentifiers(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Inputs:     []model.InputProperty{{Name: "1st-id2", Type: "string"}},
		Steps: []model.Step{{
			StepID:      "create",
			OperationID: "createOrder",
			RequestBody: &model.RequestBody{ContentType: "application/json", Payload: map[string]any{"id": "$inputs.1st-id2"}},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, `const _1st_id2 = __ENV["1st-id2"]`)
	assertContains(t, out, `"id": _1st_id2`)
}

func TestGenerateOmitsCheckWhenNoCriterionTranslates(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			SuccessCriteria: []model.SuccessCriterion{
				{Condition: `$response.body#/status == "OK"`},
				{Condition: "$statusCode == abc"},
				{Condition: "$statusCode <="},
				{Condition: "$statusCode ~ 200"},
			},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "// successCriteria (not translated): $statusCode == abc")
	assertContains(t, out, "// successCriteria (not translated): $statusCode <=")
	assertNotContains(t, out, "check(")
}

func TestGenerateFallsBackOnUnknownQualifiedSource(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "ghost", OperationID: "$sourceDescriptions.ghost.listProducts"},
			{StepID: "list", OperationID: "listProducts"},
		},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "// unresolved operationId: $sourceDescriptions.ghost.listProducts")
	// defaultBaseURL skips the unresolvable step and documents the next one.
	assertContains(t, out, "https://api.shop.test")
}

func TestGenerateFallsBackOnMalformedQualifiedRef(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps:      []model.Step{{StepID: "s", OperationID: "$sourceDescriptions.bare"}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "// unresolved operationId: $sourceDescriptions.bare")
}

func TestGenerateRendersNonStringHeaderValue(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			Parameters:  []model.Parameter{{Name: "X-Retry", In: "header", Value: 3}},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, `"X-Retry": 3`)
}

func TestJSIdentEmptyName(t *testing.T) {
	if got := jsIdent(""); got != "_" {
		t.Errorf("jsIdent(%q) = %q, want %q", "", got, "_")
	}
}

func TestJSDefaultUnmarshalableFallsBack(t *testing.T) {
	if got := jsDefault(math.NaN()); got != "''" {
		t.Errorf("jsDefault(NaN) = %q, want ''", got)
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

func TestGenerateUnescapesAndEscapesJSONPointerForGJSON(t *testing.T) {
	// RFC 6901 escapes (~1 -> /, ~0 -> ~) are decoded and gjson
	// specials in the resulting key are backslash-escaped.
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			Outputs: []model.OutputEntry{
				{Name: "path", Expression: "$response.body#/foo~1bar/a.b/q~0t"},
			},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, `const list_path = listRes.json("foo/bar.a\\.b.q~t");`)
}

func TestGenerateEscapesGJSONSyntaxCharacters(t *testing.T) {
	// Keys containing gjson syntax characters (| # and the backslash
	// escape character itself) must be escaped or the lookup silently
	// resolves to nothing.
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "list",
			OperationID: "listProducts",
			Outputs: []model.OutputEntry{
				{Name: "pipe", Expression: "$response.body#/a|b"},
				{Name: "hash", Expression: "$response.body#/a#b"},
				{Name: "bslash", Expression: `$response.body#/a\b`},
			},
		}},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, `const list_pipe = listRes.json("a\\|b");`)
	assertContains(t, out, `const list_hash = listRes.json("a\\#b");`)
	assertContains(t, out, `const list_bslash = listRes.json("a\\\\b");`)
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

func TestGenerateFlagsUntranslatableInlineExpr(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "list",
				OperationID: "listProducts",
				Parameters: []model.Parameter{
					{Name: "X-Method", In: "header", Value: "$method"},
					{Name: "trace", In: "query", Value: "id-{$request.header.x-id}"},
				},
				RequestBody: &model.RequestBody{
					ContentType: "application/json",
					Payload:     map[string]any{"self": "$self"},
				},
			},
		},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "// unsupported expression (not translated): $method")
	assertContains(t, out, "// unsupported expression (not translated): $request.header.x-id")
	assertContains(t, out, "// unsupported expression (not translated): $self")
}

func TestGenerateDoesNotFlagTranslatableInlineExpr(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Inputs:     []model.InputProperty{{Name: "cursor", Type: "string"}},
		Steps: []model.Step{
			{
				StepID:      "list",
				OperationID: "listProducts",
				Parameters: []model.Parameter{
					{Name: "cursor", In: "query", Value: "$inputs.cursor"},
				},
			},
		},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertNotContains(t, out, "// unsupported expression")
}

func TestGenerateCapturesWholeResponseBody(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "list",
				OperationID: "listProducts",
				Outputs: []model.OutputEntry{
					{Name: "all", Expression: "$response.body"},
				},
			},
		},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, "const list_all = listRes.json();")
}

func TestGenerateTranslatesDottedInputName(t *testing.T) {
	// jsIdent sanitises "user.id" to user_id consistently on both the
	// declaration (read from __ENV["user.id"]) and the reference, so a
	// dotted input name is translatable in k6 without a marker.
	wf := model.Workflow{
		WorkflowID: "wf",
		Inputs:     []model.InputProperty{{Name: "user.id", Type: "string"}},
		Steps: []model.Step{
			{
				StepID:      "list",
				OperationID: "listProducts",
				Parameters: []model.Parameter{
					{Name: "u", In: "query", Value: "$inputs.user.id"},
				},
			},
		},
	}
	out := gen(t, wf, shopSources(t), defaultOpts())
	assertContains(t, out, `const user_id = __ENV["user.id"]`)
	assertContains(t, out, "u=${user_id}")
	assertNotContains(t, out, "// unsupported expression")
}

const bodySpec = `
openapi: "3.1.0"
info: { title: B, version: "1.0.0" }
servers:
  - url: https://api.body.test
paths:
  /things:
    post:
      operationId: createThing
      requestBody:
        content:
          application/json:
            schema: { type: object }
      responses: { "201": { description: created } }
`

func TestGenerateInjectsContentTypeHeaderWhenOmitted(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "create",
			OperationID: "createThing",
			RequestBody: &model.RequestBody{
				Payload: map[string]any{"name": "value"},
			},
		}},
	}
	out := gen(t, wf, map[string]*oasresolver.Source{"b": loadSource(t, bodySpec)}, defaultOpts())
	assertContains(t, out, `"Content-Type": "application/json"`)
	assertContains(t, out, "// requestBody content-type: application/json")
}

func TestGenerateAppliesRequestBodyReplacements(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "create",
			OperationID: "createThing",
			RequestBody: &model.RequestBody{
				ContentType:  "application/json",
				Payload:      map[string]any{"name": "original"},
				Replacements: []model.Replacement{{Target: "/name", Value: "INJECTED"}},
			},
		}},
	}
	out := gen(t, wf, map[string]*oasresolver.Source{"b": loadSource(t, bodySpec)}, defaultOpts())
	assertContains(t, out, `"name": "INJECTED"`)
	assertNotContains(t, out, "original")
	assertContains(t, out, `// replacement: /name = "INJECTED"`)
}
