package parser

import (
	"errors"
	"path/filepath"
	"testing"
)

const minimalYAML = `
arazzo: "1.1.0"
info:
  title: Minimal
  version: "1.0.0"
workflows:
  - workflowId: only-step
    steps:
      - stepId: ping
        operationId: ping
`

func examplesDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "examples")
}

func TestParseMinimalYAML(t *testing.T) {
	doc, err := ParseBytes([]byte(minimalYAML))
	if err != nil {
		t.Fatalf("ParseBytes returned error: %v", err)
	}
	if doc.Arazzo != "1.1.0" {
		t.Errorf("Arazzo = %q, want %q", doc.Arazzo, "1.1.0")
	}
	if doc.Title != "Minimal" {
		t.Errorf("Title = %q, want %q", doc.Title, "Minimal")
	}
	if len(doc.Workflows) != 1 {
		t.Fatalf("len(Workflows) = %d, want 1", len(doc.Workflows))
	}
	wf := doc.Workflows[0]
	if wf.WorkflowID != "only-step" {
		t.Errorf("WorkflowID = %q, want %q", wf.WorkflowID, "only-step")
	}
	if len(wf.Inputs) != 0 {
		t.Errorf("len(Inputs) = %d, want 0", len(wf.Inputs))
	}
	if len(wf.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(wf.Steps))
	}
	if wf.Steps[0].StepID != "ping" {
		t.Errorf("StepID = %q, want %q", wf.Steps[0].StepID, "ping")
	}
	if wf.Steps[0].OperationID != "ping" {
		t.Errorf("OperationID = %q, want %q", wf.Steps[0].OperationID, "ping")
	}
}

func TestParseExampleShopYAML(t *testing.T) {
	doc, err := ParseFile(filepath.Join(examplesDir(t), "shop.arazzo.yaml"))
	if err != nil {
		t.Fatalf("ParseFile returned error: %v", err)
	}
	if doc.Title != "Shop workflows" {
		t.Errorf("Title = %q, want %q", doc.Title, "Shop workflows")
	}

	wantWorkflows := []string{"happy-path-checkout", "payment-refused-path"}
	gotWorkflows := make([]string, 0, len(doc.Workflows))
	for _, w := range doc.Workflows {
		gotWorkflows = append(gotWorkflows, w.WorkflowID)
	}
	if !equalStringSlice(gotWorkflows, wantWorkflows) {
		t.Errorf("workflow IDs = %v, want %v", gotWorkflows, wantWorkflows)
	}

	happy := doc.Workflows[0]
	wantInputs := map[string]bool{"productId": true, "orderId": true, "acceptLanguage": true}
	for _, in := range happy.Inputs {
		if !wantInputs[in.Name] {
			t.Errorf("unexpected input %q", in.Name)
		}
		delete(wantInputs, in.Name)
	}
	if len(wantInputs) != 0 {
		t.Errorf("missing inputs: %v", wantInputs)
	}

	wantStepIDs := []string{"list-catalog", "get-product", "add-to-cart", "pay"}
	gotStepIDs := make([]string, 0, len(happy.Steps))
	for _, s := range happy.Steps {
		gotStepIDs = append(gotStepIDs, s.StepID)
	}
	if !equalStringSlice(gotStepIDs, wantStepIDs) {
		t.Errorf("step IDs = %v, want %v", gotStepIDs, wantStepIDs)
	}

	listCatalog := happy.Steps[0]
	if listCatalog.OperationID != "listProducts" {
		t.Errorf("OperationID = %q, want %q", listCatalog.OperationID, "listProducts")
	}
	if len(listCatalog.Parameters) != 3 {
		t.Errorf("len(Parameters) = %d, want 3", len(listCatalog.Parameters))
	}
	if len(listCatalog.Outputs) != 1 || listCatalog.Outputs[0].Name != "firstProductId" || listCatalog.Outputs[0].Expression != "$response.body#/items/0/id" {
		t.Errorf("Outputs = %+v, want firstProductId -> $response.body#/items/0/id", listCatalog.Outputs)
	}

	getProduct := happy.Steps[1]
	if getProduct.OperationID != "getProduct" {
		t.Errorf("OperationID = %q, want %q", getProduct.OperationID, "getProduct")
	}
	if len(getProduct.Parameters) != 2 || getProduct.Parameters[0].Name != "productId" || getProduct.Parameters[0].In != "path" {
		t.Errorf("Parameters = %+v, want productId in path + Accept-Language", getProduct.Parameters)
	}

	addToCart := happy.Steps[2]
	if addToCart.RequestBody == nil {
		t.Fatal("RequestBody is nil")
	}
	if addToCart.RequestBody.ContentType != "application/json" {
		t.Errorf("ContentType = %q, want %q", addToCart.RequestBody.ContentType, "application/json")
	}
	payload, ok := addToCart.RequestBody.Payload.(map[string]any)
	if !ok {
		t.Fatalf("Payload type = %T, want map[string]any", addToCart.RequestBody.Payload)
	}
	if payload["productId"] != "$inputs.productId" {
		t.Errorf("payload.productId = %v, want $inputs.productId", payload["productId"])
	}
	if len(addToCart.SuccessCriteria) != 2 {
		t.Errorf("len(SuccessCriteria) = %d, want 2", len(addToCart.SuccessCriteria))
	}

	wantWFOutputs := map[string]string{
		"totalSpent":    "$steps.add-to-cart.outputs.cartTotal",
		"transactionId": "$steps.pay.outputs.transactionId",
	}
	gotWFOutputs := make(map[string]string, len(happy.Outputs))
	for _, o := range happy.Outputs {
		gotWFOutputs[o.Name] = o.Expression
	}
	if !equalStringMap(gotWFOutputs, wantWFOutputs) {
		t.Errorf("workflow outputs = %v, want %v", gotWFOutputs, wantWFOutputs)
	}
}

func TestArazzoParseErrorMessage(t *testing.T) {
	err := &ArazzoParseError{Msg: "boom"}
	if got := err.Error(); got != "boom" {
		t.Errorf("Error() = %q, want %q", got, "boom")
	}
}

func TestParseFileMissing(t *testing.T) {
	if _, err := ParseFile("/no/such/path.yaml"); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestParseStepOnFailureRetry(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: pay
        operationId: processPayment
        onFailure:
          - name: retry-transient
            type: retry
            stepId: pay
            retryAfter: 2000
            retryLimit: 2
            criteria:
              - condition: $statusCode >= 500
          - name: stop
            type: end
`
	doc, err := ParseBytes([]byte(src))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	step := doc.Workflows[0].Steps[0]
	if len(step.OnFailure) != 2 {
		t.Fatalf("OnFailure length = %d, want 2", len(step.OnFailure))
	}
	retry := step.OnFailure[0]
	if retry.Type != "retry" || retry.StepID != "pay" || retry.RetryAfter != 2000 || retry.RetryLimit != 2 {
		t.Errorf("retry action = %+v", retry)
	}
	if len(retry.Criteria) != 1 || retry.Criteria[0].Condition != "$statusCode >= 500" {
		t.Errorf("retry criteria = %+v", retry.Criteria)
	}
	if step.OnFailure[1].Type != "end" {
		t.Errorf("second action = %+v, want type=end", step.OnFailure[1])
	}
}

func TestParseStepOnSuccessGoto(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: a
        operationId: ping
        onSuccess:
          - name: jump-to-b
            type: goto
            stepId: b
            criteria:
              - condition: $response.body#/ok == true
      - stepId: b
        operationId: pong
`
	doc, _ := ParseBytes([]byte(src))
	first := doc.Workflows[0].Steps[0]
	if len(first.OnSuccess) != 1 {
		t.Fatalf("OnSuccess length = %d, want 1", len(first.OnSuccess))
	}
	a := first.OnSuccess[0]
	if a.Type != "goto" || a.StepID != "b" {
		t.Errorf("goto action = %+v", a)
	}
}

func TestParseRetryAfterDecimalSeconds(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: pay
        onFailure:
          - name: retry-pay
            type: retry
            retryAfter: 1.5
`
	doc, err := ParseBytes([]byte(src))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	a := doc.Workflows[0].Steps[0].OnFailure[0]
	if a.RetryAfter != 1.5 {
		t.Errorf("RetryAfter = %v, want 1.5 (spec: decimal seconds)", a.RetryAfter)
	}
	if a.RetryLimitSet {
		t.Errorf("RetryLimitSet should be false when retryLimit is absent")
	}
}

func TestParseRejectsNonMapping(t *testing.T) {
	_, err := ParseBytes([]byte("- just-a-list\n- of-strings\n"))
	var parseErr *ArazzoParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected ArazzoParseError, got %T: %v", err, err)
	}
}

func TestParseRejectsWorkflowWithoutID(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - summary: missing workflowId
    steps: []
`
	_, err := ParseBytes([]byte(src))
	var parseErr *ArazzoParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected ArazzoParseError, got %T: %v", err, err)
	}
}

func TestParseRejectsStepWithoutID(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: bad
    steps:
      - operationId: ping
`
	_, err := ParseBytes([]byte(src))
	var parseErr *ArazzoParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected ArazzoParseError, got %T: %v", err, err)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestParseRequestBodyReplacements(t *testing.T) {
	src := `
arazzo: "1.1.0"
workflows:
  - workflowId: wf
    steps:
      - stepId: create
        operationId: createThing
        requestBody:
          contentType: application/json
          payload:
            name: original
          replacements:
            - target: /name
              value: $inputs.token
            - value: orphan
`
	doc, err := ParseBytes([]byte(src))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	body := doc.Workflows[0].Steps[0].RequestBody
	if body == nil {
		t.Fatal("RequestBody is nil")
	}
	// The entry missing a target is dropped; only the valid one survives.
	if len(body.Replacements) != 1 {
		t.Fatalf("Replacements length = %d, want 1 (%+v)", len(body.Replacements), body.Replacements)
	}
	r := body.Replacements[0]
	if r.Target != "/name" || r.Value != "$inputs.token" {
		t.Errorf("replacement = %+v, want {/name $inputs.token}", r)
	}
}
