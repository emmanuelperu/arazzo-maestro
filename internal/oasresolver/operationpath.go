// operationPath support: an Arazzo step can reference an operation by
// source + JSON Pointer instead of operationId, with the form
// '{$sourceDescriptions.<name>.url}#/paths/<escaped-path>/<method>'
// (Arazzo 1.0.x, Step Object, operationPath). Only pointers addressing
// an operation under /paths are resolvable; the target operation needs
// no operationId.
package oasresolver

import (
	"fmt"
	"strings"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/emmanuelperu/arazzo-maestro/internal/expr"
)

// operationMethods is the set of HTTP methods a path item can declare,
// derived from methodOps so the two can never drift.
var operationMethods = func() map[string]bool {
	out := make(map[string]bool)
	for _, mo := range methodOps(&v3.PathItem{}) {
		out[mo.method] = true
	}
	return out
}()

// SplitOperationPath splits a step's operationPath value into the
// source-description name and the JSON Pointer after '#'. ok is false
// when the value does not match the spec form
// '{$sourceDescriptions.<name>.url}#<json-pointer>'.
func SplitOperationPath(ref string) (sourceName, pointer string, ok bool) {
	const prefix = "{$sourceDescriptions."
	const suffix = ".url}#"
	if !strings.HasPrefix(ref, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(ref, prefix)
	idx := strings.Index(rest, suffix)
	if idx < 0 {
		return "", "", false
	}
	sourceName = rest[:idx]
	if !expr.IsName(sourceName) {
		return "", "", false
	}
	return sourceName, rest[idx+len(suffix):], true
}

// OperationPathTarget decodes the HTTP method and OpenAPI path template
// an operationPath value points at, without loading any document, so
// renderers can display "GET /pet/findByStatus" instead of the raw
// reference. ok is false when the value is not the spec form or the
// pointer does not address an operation under /paths.
func OperationPathTarget(ref string) (method, path string, ok bool) {
	_, pointer, ok := SplitOperationPath(ref)
	if !ok {
		return "", "", false
	}
	return OperationPointerTarget(pointer)
}

// OperationPointerTarget decodes a '/paths/<escaped-path>/<method>' JSON
// Pointer into the method (uppercased) and the unescaped path template.
func OperationPointerTarget(pointer string) (method, path string, ok bool) {
	if !strings.HasPrefix(pointer, "/") {
		return "", "", false
	}
	segs := strings.Split(pointer[1:], "/")
	if len(segs) != 3 || segs[0] != "paths" {
		return "", "", false
	}
	method = strings.ToUpper(segs[2])
	if !operationMethods[method] {
		return "", "", false
	}
	return method, expr.UnescapeJSONPointer(segs[1]), true
}

// ResolveOperationPointer resolves the JSON Pointer of an operationPath
// against this source's operations.
func (s *Source) ResolveOperationPointer(pointer string) (Operation, error) {
	method, path, ok := OperationPointerTarget(pointer)
	if !ok {
		return Operation{}, fmt.Errorf("oasresolver: pointer %q does not address an operation (expected /paths/<escaped-path>/<method>)", pointer)
	}
	op, found := s.byPathMethod[method+" "+path]
	if !found {
		return Operation{}, fmt.Errorf("oasresolver: no %s operation on path %q", method, path)
	}
	return op, nil
}
