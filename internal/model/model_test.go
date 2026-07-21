package model

import "testing"

func TestResolvedParameters(t *testing.T) {
	params := []Parameter{
		{Name: "kept", In: "header", Value: "v"},
		{Reference: "$components.parameters.ghost"},
	}
	out := ResolvedParameters(params)
	if len(out) != 1 || out[0].Name != "kept" {
		t.Errorf("ResolvedParameters = %v, want the resolved entry only", out)
	}
}

func TestOwnParameters(t *testing.T) {
	params := []Parameter{
		{Name: "own", In: "header", Value: "v"},
		{Name: "inherited", In: "header", Value: "d", Inherited: true},
	}
	out := OwnParameters(params)
	if len(out) != 1 || out[0].Name != "own" {
		t.Errorf("OwnParameters = %v, want the step's own entry only", out)
	}
}

func TestOutputNames(t *testing.T) {
	outs := []OutputEntry{{Name: "a"}, {Name: "b"}}
	if got := OutputNames(outs); got != "a, b" {
		t.Errorf("OutputNames = %q, want %q", got, "a, b")
	}
}

func TestInlineValues(t *testing.T) {
	s := Step{
		Parameters:  []Parameter{{Name: "p", Value: "$inputs.x"}},
		RequestBody: &RequestBody{Payload: map[string]any{"a": 1}},
	}
	effective := map[string]any{"a": 2}
	got := InlineValues(s, effective)
	if len(got) != 2 {
		t.Fatalf("len(InlineValues) = %d, want 2", len(got))
	}
	if got[0] != "$inputs.x" {
		t.Errorf("InlineValues[0] = %v", got[0])
	}
	// The body slot carries the post-replacement payload, not the raw one.
	if m, ok := got[1].(map[string]any); !ok || m["a"] != 2 {
		t.Errorf("InlineValues[1] = %v, want the effective payload", got[1])
	}
}

func TestInlineValuesWithoutBody(t *testing.T) {
	s := Step{Parameters: []Parameter{{Name: "p", Value: "v"}}}
	if got := InlineValues(s, nil); len(got) != 1 {
		t.Errorf("len(InlineValues) = %d, want 1 (no body slot)", len(got))
	}
}
