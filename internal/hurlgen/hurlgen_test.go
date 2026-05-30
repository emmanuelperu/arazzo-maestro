package hurlgen

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
	assertContains(t, out, "GET https://api.shop.test/products")
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
	assertContains(t, out, "GET https://api.shop.test/products")
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
	assertContains(t, out, "GET https://api.shop.test/products/p-001")
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
	assertContains(t, out, "GET https://api.shop.test/products/{{productId}}")
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
	assertContains(t, out, "POST https://api.shop.test/orders")
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
	assertContains(t, out, `first: jsonpath "$.items.0.id"`)
	assertContains(t, out, "code: status")
	assertContains(t, out, "weird: # unsupported: $unknown.thing")
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
