package oasresolver

import "testing"

func TestSplitOperationPath(t *testing.T) {
	cases := []struct {
		name, ref, source, pointer string
		ok                         bool
	}{
		{
			name:    "canonical",
			ref:     "{$sourceDescriptions.petstore.url}#/paths/~1pet~1findByStatus/get",
			source:  "petstore",
			pointer: "/paths/~1pet~1findByStatus/get",
			ok:      true,
		},
		{
			name:    "dotted source name",
			ref:     "{$sourceDescriptions.shop.v2.url}#/paths/~1orders/post",
			source:  "shop.v2",
			pointer: "/paths/~1orders/post",
			ok:      true,
		},
		{name: "missing braces", ref: "$sourceDescriptions.petstore.url#/paths/~1pet/get"},
		{name: "missing pointer separator", ref: "{$sourceDescriptions.petstore.url}/paths/~1pet/get"},
		{name: "wrong expression", ref: "{$inputs.spec}#/paths/~1pet/get"},
		{name: "empty source name", ref: "{$sourceDescriptions..url}#/paths/~1pet/get"},
		{name: "name outside ABNF charset", ref: "{$sourceDescriptions.pet store.url}#/paths/~1pet/get"},
		{name: "empty string", ref: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, pointer, ok := SplitOperationPath(tc.ref)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if source != tc.source || pointer != tc.pointer {
				t.Errorf("got (%q, %q), want (%q, %q)", source, pointer, tc.source, tc.pointer)
			}
		})
	}
}

func TestOperationPathTarget(t *testing.T) {
	cases := []struct {
		name, ref, method, path string
		ok                      bool
	}{
		{
			name:   "escaped path",
			ref:    "{$sourceDescriptions.petstore.url}#/paths/~1pet~1findByStatus/get",
			method: "GET", path: "/pet/findByStatus", ok: true,
		},
		{
			name:   "tilde escape",
			ref:    "{$sourceDescriptions.petstore.url}#/paths/~1a~0b/post",
			method: "POST", path: "/a~b", ok: true,
		},
		{name: "pointer outside /paths", ref: "{$sourceDescriptions.petstore.url}#/components/schemas/Pet"},
		{name: "pointer to a path item", ref: "{$sourceDescriptions.petstore.url}#/paths/~1pet"},
		{name: "pointer past the operation", ref: "{$sourceDescriptions.petstore.url}#/paths/~1pet/get/responses"},
		{name: "non-method segment", ref: "{$sourceDescriptions.petstore.url}#/paths/~1pet/servers"},
		{name: "relative pointer", ref: "{$sourceDescriptions.petstore.url}#paths/~1pet/get"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method, path, ok := OperationPathTarget(tc.ref)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if method != tc.method || path != tc.path {
				t.Errorf("got (%q, %q), want (%q, %q)", method, path, tc.method, tc.path)
			}
		})
	}
}

func TestResolveOperationPointer(t *testing.T) {
	src := loadBasic(t)
	op, err := src.ResolveOperationPointer("/paths/~1users~1{id}/delete")
	if err != nil {
		t.Fatalf("ResolveOperationPointer: %v", err)
	}
	if op.Method != "DELETE" || op.Path != "/users/{id}" {
		t.Errorf("got %s %s, want DELETE /users/{id}", op.Method, op.Path)
	}
	if op.BaseURL != "https://api.example.com" {
		t.Errorf("BaseURL = %q, want doc-level server", op.BaseURL)
	}
	if op.Spec == nil || op.Spec.OperationId != "deleteUser" {
		t.Errorf("Spec not carried through: %+v", op.Spec)
	}
}

func TestResolveOperationPointerUnknownPathOrMethod(t *testing.T) {
	src := loadBasic(t)
	for name, pointer := range map[string]string{
		"unknown path":       "/paths/~1nope/get",
		"unknown method":     "/paths/~1users/patch",
		"not an op pointer":  "/components/schemas/User",
		"uppercased pointer": "/PATHS/~1users/get",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := src.ResolveOperationPointer(pointer); err == nil {
				t.Fatalf("expected error for %q, got nil", pointer)
			}
		})
	}
}

func TestResolveOperationPointerWithoutOperationID(t *testing.T) {
	// operationPath exists precisely for operations that declare no
	// operationId; they must be reachable by pointer.
	const spec = `
openapi: "3.1.0"
info: { title: NoOpID, version: "1.0.0" }
paths:
  /anonymous:
    post:
      responses: { "200": { description: ok } }
`
	src, err := Load(writeSpec(t, spec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	op, err := src.ResolveOperationPointer("/paths/~1anonymous/post")
	if err != nil {
		t.Fatalf("ResolveOperationPointer: %v", err)
	}
	if op.Method != "POST" || op.Path != "/anonymous" {
		t.Errorf("got %s %s, want POST /anonymous", op.Method, op.Path)
	}
}
