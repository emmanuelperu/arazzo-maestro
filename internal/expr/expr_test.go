package expr

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		kind       Kind
		exprName   string
		outputName string
		pointer    string
		hasPointer bool
	}{
		{name: "input", in: "$inputs.productId", kind: KindInput, exprName: "productId"},
		{name: "input with hyphen", in: "$inputs.product-id", kind: KindInput, exprName: "product-id"},
		{name: "input trimmed", in: "  $inputs.token  ", kind: KindInput, exprName: "token"},
		{name: "step output", in: "$steps.add-to-cart.outputs.cartId", kind: KindStepOutput, exprName: "add-to-cart", outputName: "cartId"},
		{name: "status code", in: "$statusCode", kind: KindStatusCode},
		{name: "response body pointer", in: "$response.body#/data/id", kind: KindResponseBody, pointer: "data/id", hasPointer: true},
		{name: "response body whole", in: "$response.body", kind: KindResponseBody},
		{name: "input with dot in name", in: "$inputs.user.name", kind: KindInput, exprName: "user.name"},
		{name: "status code prefix only", in: "$statusCodeExtra", kind: KindUnknown},
		{name: "plain literal", in: "hello", kind: KindUnknown},
		{name: "unsupported expression", in: "$method", kind: KindUnknown},
		{name: "empty", in: "", kind: KindUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.in)
			if got.Kind != tt.kind {
				t.Errorf("Kind = %v, want %v", got.Kind, tt.kind)
			}
			if got.Name != tt.exprName {
				t.Errorf("Name = %q, want %q", got.Name, tt.exprName)
			}
			if got.OutputName != tt.outputName {
				t.Errorf("OutputName = %q, want %q", got.OutputName, tt.outputName)
			}
			if got.Pointer != tt.pointer {
				t.Errorf("Pointer = %q, want %q", got.Pointer, tt.pointer)
			}
			if got.HasPointer != tt.hasPointer {
				t.Errorf("HasPointer = %v, want %v", got.HasPointer, tt.hasPointer)
			}
			if got.Raw != tt.in {
				t.Errorf("Raw = %q, want %q", got.Raw, tt.in)
			}
		})
	}
}

func TestIsName(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"productId", true},
		{"product-id", true},
		{"product_id", true},
		{"abc123", true},
		{"user.name", true},
		{"", false},
		{"a/b", false},
		{"a#b", false},
	}
	for _, tt := range tests {
		if got := IsName(tt.in); got != tt.want {
			t.Errorf("IsName(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestRefs(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"$inputs.foo", []string{"$inputs.foo"}},
		{"  $statusCode ", []string{"$statusCode"}},
		{"Bearer {$inputs.token}", []string{"$inputs.token"}},
		{"a {$x} b {$y}", []string{"$x", "$y"}},
		{"plain literal", nil},
	}
	for _, tt := range tests {
		got := Refs(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("Refs(%q) = %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("Refs(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestCollectRefs(t *testing.T) {
	v := map[string]any{
		"b": "$method",
		"a": "$inputs.id",
		"nested": map[string]any{
			"z": []any{"$inputs.id", "literal", "Bearer {$inputs.token}"},
		},
	}
	// Deterministic: map keys visited sorted ("a" before "b" before "nested"),
	// duplicates removed ($inputs.id appears twice).
	got := CollectRefs(v)
	want := []string{"$inputs.id", "$method", "$inputs.token"}
	if len(got) != len(want) {
		t.Fatalf("CollectRefs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("CollectRefs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestIsRuntimeExpression(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"$inputs.foo", true},
		{"$method", true},
		{"  $statusCode", true},
		{"hello", false},
		{"", false},
		{"price is $5", false},
	}
	for _, tt := range tests {
		if got := IsRuntimeExpression(tt.in); got != tt.want {
			t.Errorf("IsRuntimeExpression(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
