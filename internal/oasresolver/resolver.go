// Package oasresolver loads a local OpenAPI 3.x document and resolves
// operationIds to their HTTP method, path template, and effective base
// server URL.
//
// Backed by github.com/pb33f/libopenapi. Only local files are loaded.
// Rejection of HTTP/HTTPS sources is left to the caller.
package oasresolver

import (
	"fmt"
	"os"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

// Operation is a resolved view of an OpenAPI operation. Method, Path
// and BaseURL flatten data that lives outside `v3.Operation` (on the
// parent PathItem and on the document's servers). Spec re-exposes the
// libopenapi op so callers can read Parameters, RequestBody, Responses,
// etc. without going back through the document.
type Operation struct {
	Method  string
	Path    string
	BaseURL string // op > path > document fallback chain, "" if none of the levels declares a server
	Spec    *v3.Operation
}

// Source is a loaded OpenAPI document, indexed by operationId.
type Source struct {
	doc    *v3.Document
	byOpID map[string]Operation
}

// Load parses a local OpenAPI 3.x file and indexes its operations.
func Load(path string) (*Source, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("oasresolver: read %s: %w", path, err)
	}
	doc, err := libopenapi.NewDocument(raw)
	if err != nil {
		return nil, fmt.Errorf("oasresolver: parse %s: %w", path, err)
	}
	model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("oasresolver: build model from %s: %w", path, err)
	}
	s := &Source{doc: &model.Model}
	s.indexOperations()
	return s, nil
}

// Resolve returns the operation matching operationID.
func (s *Source) Resolve(operationID string) (Operation, error) {
	op, ok := s.byOpID[operationID]
	if !ok {
		return Operation{}, fmt.Errorf("oasresolver: operationId %q not found", operationID)
	}
	return op, nil
}

// HasOperationID reports whether the source declares an operation with
// this id.
func (s *Source) HasOperationID(operationID string) bool {
	_, ok := s.byOpID[operationID]
	return ok
}

// OperationIDs returns the set of operationIds declared in the source.
func (s *Source) OperationIDs() map[string]bool {
	out := make(map[string]bool, len(s.byOpID))
	for id := range s.byOpID {
		out[id] = true
	}
	return out
}

func (s *Source) indexOperations() {
	s.byOpID = make(map[string]Operation)
	docBase := firstServerURL(s.doc.Servers)

	if s.doc.Paths == nil || s.doc.Paths.PathItems == nil {
		return
	}
	for entry := s.doc.Paths.PathItems.First(); entry != nil; entry = entry.Next() {
		path := entry.Key()
		item := entry.Value()
		pathBase := firstServerURL(item.Servers)
		if pathBase == "" {
			pathBase = docBase
		}
		for _, mo := range methodOps(item) {
			if mo.op == nil || mo.op.OperationId == "" {
				continue
			}
			opBase := firstServerURL(mo.op.Servers)
			if opBase == "" {
				opBase = pathBase
			}
			// First definition wins on duplicate operationId.
			if _, exists := s.byOpID[mo.op.OperationId]; exists {
				continue
			}
			s.byOpID[mo.op.OperationId] = Operation{
				Method:  mo.method,
				Path:    path,
				BaseURL: opBase,
				Spec:    mo.op,
			}
		}
	}
}

// methodOp pairs an HTTP verb with its operation pointer. A slice keeps
// iteration deterministic so the "first wins on duplicate" rule above
// is reproducible.
type methodOp struct {
	method string
	op     *v3.Operation
}

func methodOps(item *v3.PathItem) []methodOp {
	return []methodOp{
		{"GET", item.Get},
		{"PUT", item.Put},
		{"POST", item.Post},
		{"DELETE", item.Delete},
		{"OPTIONS", item.Options},
		{"HEAD", item.Head},
		{"PATCH", item.Patch},
		{"TRACE", item.Trace},
		{"QUERY", item.Query},
	}
}

func firstServerURL(servers []*v3.Server) string {
	if len(servers) == 0 {
		return ""
	}
	return servers[0].URL
}
