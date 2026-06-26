package payload

import (
	"reflect"
	"testing"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
)

func TestApplySetsValues(t *testing.T) {
	tests := []struct {
		name  string
		root  any
		repls []model.Replacement
		want  any
		unres []string
	}{
		{
			name:  "object key",
			root:  map[string]any{"a": 1, "b": 2},
			repls: []model.Replacement{{Target: "/a", Value: 9}},
			want:  map[string]any{"a": 9, "b": 2},
		},
		{
			name:  "nested key",
			root:  map[string]any{"a": map[string]any{"b": 1}},
			repls: []model.Replacement{{Target: "/a/b", Value: "x"}},
			want:  map[string]any{"a": map[string]any{"b": "x"}},
		},
		{
			name:  "array index",
			root:  map[string]any{"items": []any{10, 20, 30}},
			repls: []model.Replacement{{Target: "/items/1", Value: 99}},
			want:  map[string]any{"items": []any{10, 99, 30}},
		},
		{
			name:  "add absent key",
			root:  map[string]any{"a": 1},
			repls: []model.Replacement{{Target: "/b", Value: 2}},
			want:  map[string]any{"a": 1, "b": 2},
		},
		{
			name:  "whole document",
			root:  map[string]any{"a": 1},
			repls: []model.Replacement{{Target: "", Value: "replaced"}},
			want:  "replaced",
		},
		{
			name:  "unresolved missing parent",
			root:  map[string]any{"a": 1},
			repls: []model.Replacement{{Target: "/missing/x", Value: 1}},
			want:  map[string]any{"a": 1},
			unres: []string{"/missing/x"},
		},
		{
			name:  "unresolved out-of-range index",
			root:  map[string]any{"items": []any{1}},
			repls: []model.Replacement{{Target: "/items/5", Value: 1}},
			want:  map[string]any{"items": []any{1}},
			unres: []string{"/items/5"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, unres := Apply(tt.root, tt.repls)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Apply() = %#v, want %#v", got, tt.want)
			}
			if !reflect.DeepEqual(unres, tt.unres) {
				t.Errorf("unresolved = %#v, want %#v", unres, tt.unres)
			}
		})
	}
}

func TestApplyDoesNotMutateInput(t *testing.T) {
	root := map[string]any{"a": map[string]any{"b": 1}}
	_, _ = Apply(root, []model.Replacement{{Target: "/a/b", Value: 2}})
	inner := root["a"].(map[string]any)
	if inner["b"] != 1 {
		t.Errorf("input mutated: a.b = %v, want 1", inner["b"])
	}
}
