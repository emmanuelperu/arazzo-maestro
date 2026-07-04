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
	"strings"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
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

// RequestContentTypes returns the media types declared on the
// operation's request body, in declaration order. It is empty when the
// operation declares no request body.
func (o Operation) RequestContentTypes() []string {
	if o.Spec == nil || o.Spec.RequestBody == nil || o.Spec.RequestBody.Content == nil {
		return nil
	}
	var out []string
	for e := o.Spec.RequestBody.Content.First(); e != nil; e = e.Next() {
		out = append(out, e.Key())
	}
	return out
}

// EffectiveContentType picks the content type a generated request should
// use, following the Arazzo rule that an omitted requestBody.contentType
// defers to the targeted operation (Request Body Object, contentType).
// It returns the explicit Arazzo type when set; otherwise the operation's
// type when it declares exactly one; otherwise application/json when the
// operation declares it among several; otherwise ("", false) when the
// type cannot be determined.
func EffectiveContentType(explicit string, declared []string) (string, bool) {
	if strings.TrimSpace(explicit) != "" {
		return explicit, true
	}
	switch len(declared) {
	case 0:
		return "", false
	case 1:
		return declared[0], true
	}
	for _, ct := range declared {
		if isJSONMediaType(ct) {
			return ct, true
		}
	}
	return "", false
}

// isJSONMediaType reports whether a media type is JSON: application/json
// or any structured +json suffix (e.g. application/vnd.api+json),
// ignoring parameters like "; charset=utf-8".
func isJSONMediaType(ct string) bool {
	base := ct
	if i := strings.IndexByte(base, ';'); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimSpace(strings.ToLower(base))
	return base == "application/json" || strings.HasSuffix(base, "+json")
}

// Source is a loaded OpenAPI document, indexed by operationId and by
// "<METHOD> <path>" (operationPath references target operations that may
// declare no operationId at all).
type Source struct {
	doc          *v3.Document
	byOpID       map[string]Operation
	byPathMethod map[string]Operation
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

// ResolveStepOperation resolves a step's operation reference,
// operationId or operationPath, whichever the step declares (the schema
// enforces their mutual exclusivity upstream). sources is keyed by
// sourceDescription name. Both test generators and their base-URL
// lookups share this dispatch so the same Arazzo step can never resolve
// differently per output format.
func ResolveStepOperation(step model.Step, sources map[string]*Source) (Operation, bool) {
	switch {
	case step.OperationID != "":
		srcName, opID := splitOperationRef(step.OperationID, sources)
		src, exists := sources[srcName]
		if srcName == "" || !exists {
			return Operation{}, false
		}
		op, err := src.Resolve(opID)
		return op, err == nil
	case step.OperationPath != "":
		srcName, pointer, valid := SplitOperationPath(step.OperationPath)
		src, exists := sources[srcName]
		if !valid || !exists {
			return Operation{}, false
		}
		op, err := src.ResolveOperationPointer(pointer)
		return op, err == nil
	}
	return Operation{}, false
}

// splitOperationRef recognises the two accepted forms of operationId:
//
//	"createOrder"                              -> short form
//	"$sourceDescriptions.shop-api.createOrder" -> qualified form
//
// Short form resolves only when exactly one source is configured; when
// multiple sources are present, the caller is expected to have used the
// qualified form (the linter enforces this upstream).
func splitOperationRef(ref string, sources map[string]*Source) (srcName, opID string) {
	const prefix = "$sourceDescriptions."
	if strings.HasPrefix(ref, prefix) {
		rest := strings.TrimPrefix(ref, prefix)
		idx := strings.Index(rest, ".")
		if idx < 0 {
			return "", ""
		}
		return rest[:idx], rest[idx+1:]
	}
	if len(sources) == 1 {
		for name := range sources {
			return name, ref
		}
	}
	return "", ""
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
	s.byPathMethod = make(map[string]Operation)
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
			if mo.op == nil {
				continue
			}
			opBase := firstServerURL(mo.op.Servers)
			if opBase == "" {
				opBase = pathBase
			}
			op := Operation{
				Method:  mo.method,
				Path:    path,
				BaseURL: opBase,
				Spec:    mo.op,
			}
			s.byPathMethod[mo.method+" "+path] = op
			if mo.op.OperationId == "" {
				continue
			}
			// First definition wins on duplicate operationId.
			if _, exists := s.byOpID[mo.op.OperationId]; exists {
				continue
			}
			s.byOpID[mo.op.OperationId] = op
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
