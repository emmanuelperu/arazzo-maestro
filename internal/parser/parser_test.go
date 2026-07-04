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

func TestParseStepOperationPathAndWorkflowID(t *testing.T) {
	const src = `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
workflows:
  - workflowId: wf
    steps:
      - stepId: by-path
        operationPath: '{$sourceDescriptions.petstore.url}#/paths/~1pet~1findByStatus/get'
      - stepId: by-workflow
        workflowId: other-flow
`
	doc, err := ParseBytes([]byte(src))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	steps := doc.Workflows[0].Steps
	if len(steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2", len(steps))
	}
	if got, want := steps[0].OperationPath, "{$sourceDescriptions.petstore.url}#/paths/~1pet~1findByStatus/get"; got != want {
		t.Errorf("OperationPath = %q, want %q", got, want)
	}
	if steps[0].WorkflowID != "" || steps[0].OperationID != "" {
		t.Errorf("step by-path: unexpected WorkflowID %q / OperationID %q", steps[0].WorkflowID, steps[0].OperationID)
	}
	if got, want := steps[1].WorkflowID, "other-flow"; got != want {
		t.Errorf("WorkflowID = %q, want %q", got, want)
	}
	if steps[1].OperationPath != "" || steps[1].OperationID != "" {
		t.Errorf("step by-workflow: unexpected OperationPath %q / OperationID %q", steps[1].OperationPath, steps[1].OperationID)
	}
}

const componentsYAML = `
arazzo: "1.1.0"
info: { title: t, version: "1.0.0" }
components:
  parameters:
    page-size:
      name: pageSize
      in: query
      value: 20
    broken: not-a-mapping
  successActions:
    finish:
      name: finish
      type: end
  failureActions:
    retry-later:
      name: retry-later
      type: retry
      retryAfter: 2.5
      retryLimit: 3
workflows:
  - workflowId: wf
    steps:
      - stepId: list
        operationId: listThings
        parameters:
          - reference: $components.parameters.page-size
          - reference: $components.parameters.page-size
            value: 50
          - reference: $components.parameters.ghost
        onSuccess:
          - reference: $components.successActions.finish
        onFailure:
          - reference: $components.failureActions.retry-later
`

func TestParseComponentsAndResolveReusableRefs(t *testing.T) {
	doc, err := ParseBytes([]byte(componentsYAML))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	// The non-mapping definition is skipped, only page-size survives.
	if len(doc.Components.Parameters) != 1 || len(doc.Components.SuccessActions) != 1 || len(doc.Components.FailureActions) != 1 {
		t.Fatalf("components not parsed: %+v", doc.Components)
	}
	step := doc.Workflows[0].Steps[0]
	if len(step.Parameters) != 3 {
		t.Fatalf("len(Parameters) = %d, want 3", len(step.Parameters))
	}
	// Plain reference: inlined as declared in components.
	p := step.Parameters[0]
	if p.Name != "pageSize" || p.In != "query" || p.Value != int64(20) {
		t.Errorf("inlined parameter = %+v, want pageSize/query/20", p)
	}
	if p.Reference != "" {
		t.Errorf("Reference must be cleared on resolution, got %q", p.Reference)
	}
	// Reusable value overrides the component's value.
	if v := step.Parameters[1].Value; v != int64(50) {
		t.Errorf("override value = %v, want 50", v)
	}
	if step.Parameters[1].Name != "pageSize" {
		t.Errorf("override kept component name: %+v", step.Parameters[1])
	}
	// Dangling reference: left unresolved for the linter to flag.
	if p := step.Parameters[2]; p.Name != "" || p.Reference != "$components.parameters.ghost" {
		t.Errorf("dangling ref should stay unresolved, got %+v", p)
	}
	// Actions inline the same way, clearing Reference.
	if a := step.OnSuccess[0]; a.Type != "end" || a.Name != "finish" || a.Reference != "" {
		t.Errorf("onSuccess not inlined: %+v", a)
	}
	fa := step.OnFailure[0]
	if fa.Type != "retry" || fa.RetryAfter != 2.5 || fa.RetryLimit != 3 || !fa.RetryLimitSet {
		t.Errorf("onFailure not inlined: %+v", fa)
	}
}

func TestComponentRefName(t *testing.T) {
	cases := []struct {
		ref, kind, name string
		ok              bool
	}{
		{"$components.parameters.page-size", "parameters", "page-size", true},
		{"$components.parameters.a.b", "parameters", "a.b", true},
		{"$components.successActions.finish", "successActions", "finish", true},
		{"$components.successActions.finish", "failureActions", "", false},
		{"$components.parameters.", "parameters", "", false},
		{"$inputs.pageSize", "parameters", "", false},
		{"", "parameters", "", false},
	}
	for _, tc := range cases {
		name, ok := ComponentRefName(tc.ref, tc.kind)
		if name != tc.name || ok != tc.ok {
			t.Errorf("ComponentRefName(%q, %q) = (%q, %v), want (%q, %v)", tc.ref, tc.kind, name, ok, tc.name, tc.ok)
		}
	}
}
