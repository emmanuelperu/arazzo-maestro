package hurlgen

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

func TestGenerateEmptyWorkflowProducesHeaderOnly(t *testing.T) {
	wf := model.Workflow{WorkflowID: "empty"}
	out, err := Generate(wf, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	assertContains(t, out, "# Workflow: empty")
	assertNotContains(t, out, "# Step:")
}

func TestGenerateHeaderIncludesSummaryAndInputs(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "checkout",
		Summary:    "Buy a thing",
		Inputs: []model.InputProperty{
			{Name: "productId", Type: "string"},
			{Name: "qty", Type: "integer"},
		},
	}
	out, _ := Generate(wf, nil)
	assertContains(t, out, "# Workflow: checkout")
	assertContains(t, out, "# Buy a thing")
	assertContains(t, out, "# Inputs")
	assertContains(t, out, "- productId (string)")
	assertContains(t, out, "- qty (integer)")
	// The baseUrl variable is always advertised, even with no resolvable
	// source to supply a default.
	assertContains(t, out, "# Base URL (required)")
}

func TestGenerateDocumentsDefaultBaseURLFromServers(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "list", OperationID: "listProducts"},
		},
	}
	sources := map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	}
	out, _ := Generate(wf, sources)
	// The request line stays environment-agnostic...
	assertContains(t, out, "GET {{baseUrl}}/products")
	assertNotContains(t, out, "https://api.shop.test/products")
	// ...while the OpenAPI servers URL is surfaced as the documented default.
	assertContains(t, out, "default (OpenAPI servers): https://api.shop.test")
}

func TestGenerateResolvesShortFormOperationIDAgainstSingleSource(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "list", OperationID: "listProducts"},
		},
	}
	sources := map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	}
	out, _ := Generate(wf, sources)
	assertContains(t, out, "GET {{baseUrl}}/products")
	assertNotContains(t, out, "__unresolved__")
}

func TestGenerateResolvesQualifiedOperationID(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "list", OperationID: "$sourceDescriptions.shop.listProducts"},
		},
	}
	sources := map[string]*oasresolver.Source{
		"shop":  loadSource(t, shopSpec),
		"other": loadSource(t, noServerSpec),
	}
	out, _ := Generate(wf, sources)
	assertContains(t, out, "GET {{baseUrl}}/products")
	assertNotContains(t, out, "__unresolved__")
}

func TestGenerateFallsBackToPlaceholderWhenSourceMissing(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "list", OperationID: "$sourceDescriptions.unknown.listProducts"},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, "# unresolved operationId: $sourceDescriptions.unknown.listProducts")
	assertContains(t, out, "{{baseUrl}}/__unresolved__/")
}

func TestGenerateFallsBackOnShortFormWithMultipleSources(t *testing.T) {
	// The linter rejects this case upstream; hurlgen still must not
	// crash and must produce a placeholder line so the file stays
	// valid Hurl.
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "list", OperationID: "listProducts"},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop":  loadSource(t, shopSpec),
		"other": loadSource(t, noServerSpec),
	})
	assertContains(t, out, "# unresolved operationId: listProducts")
}

func TestGenerateFallsBackOnUnknownOperationID(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "ghost", OperationID: "noSuchOp"},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, "# unresolved operationId: noSuchOp")
}

func TestGenerateFallsBackOnMalformedQualifiedRef(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "x", OperationID: "$sourceDescriptions.shop"},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, "# unresolved operationId: $sourceDescriptions.shop")
}

func TestGenerateAcceptsNilSourcesMap(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "x", OperationID: "anything"},
		},
	}
	out, err := Generate(wf, nil)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	assertContains(t, out, "# unresolved operationId: anything")
}

func TestGenerateSubstitutesPathParameters(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "get",
				OperationID: "getProduct",
				Parameters: []model.Parameter{
					{Name: "id", In: "path", Value: "p-001"},
				},
			},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, "GET {{baseUrl}}/products/p-001")
	assertNotContains(t, out, "{id}")
}

func TestGenerateSubstitutesPathParameterFromRuntimeExpression(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "get",
				OperationID: "getProduct",
				Parameters: []model.Parameter{
					{Name: "id", In: "path", Value: "$inputs.productId"},
				},
			},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, "GET {{baseUrl}}/products/{{productId}}")
}

func TestGenerateEmitsQueryAndHeaderParameters(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
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
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, "Authorization: Bearer x")
	assertContains(t, out, "[QueryStringParams]")
	assertContains(t, out, "limit: 10")
	assertContains(t, out, "cursor: {{prev_next}}")
}

func TestGenerateEmitsRequestBody(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "create",
				OperationID: "createOrder",
				RequestBody: &model.RequestBody{
					ContentType: "application/json",
					Payload:     "...payload...",
				},
			},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, "POST {{baseUrl}}/orders")
	assertContains(t, out, "# requestBody content-type: application/json")
	assertContains(t, out, "...payload...")
}

func TestGenerateMarshalsMapBodyAsJSON(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "create",
				OperationID: "createOrder",
				RequestBody: &model.RequestBody{
					ContentType: "application/json",
					Payload: map[string]any{
						"amount":   49.99,
						"currency": "EUR",
					},
				},
			},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, `"amount": 49.99`)
	assertContains(t, out, `"currency": "EUR"`)
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
			want:    []string{`"productId": "{{productId}}"`, `"quantity": 2`},
			notWant: []string{"$inputs.productId"},
		},
		{
			name:    "step output reference",
			payload: map[string]any{"cartId": "$steps.add-to-cart.outputs.cartId"},
			want:    []string{`"cartId": "{{add-to-cart_cartId}}"`},
		},
		{
			name:    "nested object",
			payload: map[string]any{"customer": map[string]any{"id": "$inputs.customerId"}},
			want:    []string{`"id": "{{customerId}}"`},
		},
		{
			name: "nested array",
			payload: map[string]any{"items": []any{
				"$inputs.productId",
				map[string]any{"ref": "$steps.list.outputs.first"},
			}},
			want: []string{`"{{productId}}"`, `"ref": "{{list_first}}"`},
		},
		{
			name:    "unrecognised expression form passes through",
			payload: map[string]any{"note": "$response.body#/x"},
			want:    []string{`"note": "$response.body#/x"`},
		},
		{
			name:    "raw string payload that is an expression",
			payload: "$inputs.rawBody",
			want:    []string{"{{rawBody}}"},
			notWant: []string{"$inputs.rawBody"},
		},
		{
			name:    "free text after the inputs prefix stays a literal",
			payload: map[string]any{"note": "$inputs.tax is included"},
			want:    []string{`"note": "$inputs.tax is included"`},
			notWant: []string{"{{tax is included}}"},
		},
		{
			name:    "dotted input sub-path stays a literal",
			payload: map[string]any{"name": "$inputs.user.name"},
			want:    []string{`"name": "$inputs.user.name"`},
			notWant: []string{"{{user.name}}"},
		},
		{
			name:    "dotted step output sub-path stays a literal",
			payload: map[string]any{"ref": "$steps.s.outputs.user.name"},
			want:    []string{`"ref": "$steps.s.outputs.user.name"`},
			notWant: []string{"{{s_user.name}}"},
		},
		{
			name:    "empty name after the inputs prefix stays a literal",
			payload: map[string]any{"id": "$inputs."},
			want:    []string{`"id": "$inputs."`},
		},
		{
			name:    "step reference without an outputs segment stays a literal",
			payload: map[string]any{"id": "$steps.foo.bar"},
			want:    []string{`"id": "$steps.foo.bar"`},
		},
		{
			name:    "expression embedded in surrounding text stays a literal",
			payload: map[string]any{"auth": "Bearer $inputs.productId"},
			want:    []string{`"auth": "Bearer $inputs.productId"`},
		},
		{
			name:    "literal sentinel-looking string does not steal the swap",
			payload: map[string]any{"a": "__arazzo_tpl_0__", "b": "$inputs.productId"},
			want:    []string{`"a": "__arazzo_tpl_0__"`, `"b": "{{productId}}"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := model.Workflow{
				WorkflowID: "wf",
				Steps: []model.Step{
					{
						StepID:      "create",
						OperationID: "createOrder",
						RequestBody: &model.RequestBody{ContentType: "application/json", Payload: tt.payload},
					},
				},
			}
			out, _ := Generate(wf, map[string]*oasresolver.Source{
				"shop": loadSource(t, shopSpec),
			})
			for _, w := range tt.want {
				assertContains(t, out, w)
			}
			for _, nw := range tt.notWant {
				assertNotContains(t, out, nw)
			}
		})
	}
}

func TestGenerateUnquotesNonStringInputTemplatesInBody(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Inputs: []model.InputProperty{
			{Name: "qty", Type: "integer"},
			{Name: "productId", Type: "string"},
		},
		Steps: []model.Step{{
			StepID:      "create",
			OperationID: "createOrder",
			RequestBody: &model.RequestBody{
				ContentType: "application/json",
				Payload:     map[string]any{"quantity": "$inputs.qty", "productId": "$inputs.productId"},
			},
		}},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{"shop": loadSource(t, shopSpec)})
	assertContains(t, out, `"quantity": {{qty}}`)
	assertContains(t, out, `"productId": "{{productId}}"`)
}

func TestGenerateKeepsLiteralTemplateLookingBodyStrings(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Inputs:     []model.InputProperty{{Name: "qty", Type: "integer"}},
		Steps: []model.Step{{
			StepID:      "create",
			OperationID: "createOrder",
			RequestBody: &model.RequestBody{
				ContentType: "application/json",
				// "label" is literal data that merely looks like a template.
				Payload: map[string]any{"label": "{{qty}}", "quantity": "$inputs.qty"},
			},
		}},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{"shop": loadSource(t, shopSpec)})
	assertContains(t, out, `"label": "{{qty}}"`)
	assertContains(t, out, `"quantity": {{qty}}`)
}

func TestGenerateFallsBackWhenJSONBodyUnmarshalable(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "create",
			OperationID: "createOrder",
			RequestBody: &model.RequestBody{
				ContentType: "application/json",
				Payload:     map[string]any{"bad": math.NaN()},
			},
		}},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{"shop": loadSource(t, shopSpec)})
	assertContains(t, out, "map[bad:NaN]")
}

func TestGenerateTranslatesExprsInNonJSONBodyDump(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{{
			StepID:      "create",
			OperationID: "createOrder",
			RequestBody: &model.RequestBody{
				ContentType: "text/plain",
				Payload: map[string]any{
					"id":   "$inputs.productId",
					"tags": []any{"$steps.s.outputs.o"},
				},
			},
		}},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{"shop": loadSource(t, shopSpec)})
	assertContains(t, out, "{{productId}}")
	assertContains(t, out, "{{s_o}}")
	assertContains(t, out, "map[")
}

func TestGenerateFallsBackToGoFormattingForUnknownContentType(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "create",
				OperationID: "createOrder",
				RequestBody: &model.RequestBody{
					ContentType: "application/x-mystery",
					Payload:     map[string]any{"k": "v"},
				},
			},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, "# requestBody content-type: application/x-mystery")
	assertContains(t, out, "map[k:v]")
}

func TestGenerateEmitsOutputsAsCaptures(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "list",
				OperationID: "listProducts",
				Outputs: []model.OutputEntry{
					{Name: "first", Expression: "$response.body#/items/0/id"},
					{Name: "code", Expression: "$statusCode"},
					{Name: "weird", Expression: "$unknown.thing"},
				},
			},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, "[Captures]")
	// Capture names are namespaced by step id so the chain
	// $steps.list.outputs.first → {{list_first}} resolves.
	// Numeric JSON Pointer segments must be emitted as array
	// indexers [0] (not .0) to be valid JSONPath.
	assertContains(t, out, `list_first: jsonpath "$.items[0].id"`)
	assertContains(t, out, "list_code: status")
	assertContains(t, out, "list_weird: # unsupported: $unknown.thing")
}

func TestGenerateHandlesEmptyJSONPointerSegment(t *testing.T) {
	// Double-slash in the pointer produces an empty segment; it must
	// be treated as a named (dotted) segment, not as a numeric index.
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "list",
				OperationID: "listProducts",
				Outputs: []model.OutputEntry{
					{Name: "weird", Expression: "$response.body#/items//id"},
				},
			},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, `list_weird: jsonpath "$.items..id"`)
}

func TestGenerateChainsStepCaptureToLaterStepReference(t *testing.T) {
	// The whole point of generating Hurl rather than a bag of
	// unrelated requests: a capture defined in step A must produce a
	// Hurl variable whose name matches what step B's reference
	// translation expects, otherwise hurl --test fails at runtime
	// with 'undefined variable'.
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "auth",
				OperationID: "listProducts",
				Outputs: []model.OutputEntry{
					{Name: "token", Expression: "$response.body#/token"},
				},
			},
			{
				StepID:      "use",
				OperationID: "createOrder",
				Parameters: []model.Parameter{
					{Name: "Authorization", In: "header", Value: "$steps.auth.outputs.token"},
				},
			},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, `auth_token: jsonpath "$.token"`)
	assertContains(t, out, "Authorization: {{auth_token}}")
}

func TestGenerateEmitsSuccessCriteriaAsAssertComments(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "list",
				OperationID: "listProducts",
				SuccessCriteria: []model.SuccessCriterion{
					{Condition: "$statusCode == 200"},
					{Condition: "$response.body#/items != []"},
				},
			},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, "[Asserts]")
	assertContains(t, out, "# $statusCode == 200")
	assertContains(t, out, "# $response.body#/items != []")
}

func TestGenerateFallsBackToBaseUrlPlaceholderWhenServerMissing(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "ping", OperationID: "ping"},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"noServer": loadSource(t, noServerSpec),
	})
	assertContains(t, out, "GET {{baseUrl}}/ping")
}

func TestGenerateRendersStepDescription(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{
				StepID:      "list",
				Description: "Get the catalog.\nUsed by the homepage.",
				OperationID: "listProducts",
			},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, "# Get the catalog.")
	assertContains(t, out, "# Used by the homepage.")
}

func TestGenerateSeparatesMultipleSteps(t *testing.T) {
	wf := model.Workflow{
		WorkflowID: "wf",
		Steps: []model.Step{
			{StepID: "first", OperationID: "listProducts"},
			{StepID: "second", OperationID: "createOrder"},
		},
	}
	out, _ := Generate(wf, map[string]*oasresolver.Source{
		"shop": loadSource(t, shopSpec),
	})
	assertContains(t, out, "# Step: first")
	assertContains(t, out, "# Step: second")
	assertContains(t, out, "\n\n# Step: second")
}
