// Package expr parses Arazzo runtime expressions into a small typed form
// shared by the test generators. It centralises recognition of the
// expression grammar; each generator keeps its own output formatting
// (Hurl templates in hurlgen, JS identifiers in k6gen), so the parsing
// rules live in one place instead of being duplicated per target.
//
// The recognised forms mirror the Arazzo 1.1.0 runtime-expression ABNF
// the generators can translate; anything else is reported as
// KindUnknown so callers can decide how to surface it.
package expr

import (
	"regexp"
	"sort"
	"strings"
)

// Kind classifies a runtime expression.
type Kind int

const (
	// KindUnknown is any string the parser does not recognise as a
	// supported runtime-expression form, including plain literals.
	KindUnknown Kind = iota
	// KindInput is $inputs.<name>.
	KindInput
	// KindStepOutput is $steps.<step>.outputs.<output>.
	KindStepOutput
	// KindStatusCode is $statusCode.
	KindStatusCode
	// KindResponseBody is $response.body, with or without a #/<pointer>
	// suffix (the pointer is optional per the ABNF).
	KindResponseBody
)

// Expr is a parsed runtime expression.
type Expr struct {
	Kind       Kind
	Name       string // input name, or step id for KindStepOutput
	OutputName string // output name for KindStepOutput
	Pointer    string // JSON-pointer body after "#/", set when HasPointer
	HasPointer bool
	Raw        string // the original input, untrimmed
}

// Parse recognises the runtime-expression forms the generators translate.
// Unrecognised input (including plain literals) yields KindUnknown with
// the original string preserved in Raw. The spec applies the same
// #/<json-pointer> sub-access to $inputs and $steps references in its
// examples (e.g. $steps.someStepId.outputs.pets#/0/id), so those forms
// carry an optional pointer like $response.body does.
func Parse(s string) Expr {
	e := strings.TrimSpace(s)
	base, pointer, hasPointer := splitPointer(e)
	switch {
	case e == "$statusCode":
		return Expr{Kind: KindStatusCode, Raw: s}
	case base == "$response.body":
		return Expr{Kind: KindResponseBody, Pointer: pointer, HasPointer: hasPointer, Raw: s}
	case strings.HasPrefix(base, "$inputs."):
		if name := strings.TrimPrefix(base, "$inputs."); IsName(name) {
			return Expr{Kind: KindInput, Name: name, Pointer: pointer, HasPointer: hasPointer, Raw: s}
		}
	case strings.HasPrefix(base, "$steps."):
		rest := strings.TrimPrefix(base, "$steps.")
		if step, out, ok := splitStepOutput(rest); ok && IsName(step) && IsName(out) {
			return Expr{Kind: KindStepOutput, Name: step, OutputName: out, Pointer: pointer, HasPointer: hasPointer, Raw: s}
		}
	}
	return Expr{Kind: KindUnknown, Raw: s}
}

// splitPointer separates a runtime expression from its optional
// #/<json-pointer> suffix. A bare "#" (the RFC 6901 whole-document
// pointer) is treated as no pointer at all; anything after "#" that is
// not a valid pointer start keeps the expression unrecognised.
func splitPointer(e string) (base, pointer string, ok bool) {
	idx := strings.IndexByte(e, '#')
	if idx < 0 || idx == len(e)-1 {
		return strings.TrimSuffix(e, "#"), "", false
	}
	if e[idx+1] != '/' {
		return e, "", false
	}
	return e[:idx], e[idx+2:], true
}

// IsRuntimeExpression reports whether s is (the start of) an Arazzo
// runtime expression: a "$"-prefixed token. It does not validate the
// form, only the sigil, so callers can distinguish an unsupported
// expression from plain literal data.
func IsRuntimeExpression(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "$")
}

// IsName reports whether s is an Arazzo name. The runtime-expression
// ABNF allows letters, digits, '_', '-' and '.' in a name; a sub-access
// into a value uses the separate #/<json-pointer> suffix, so a '.' here
// is part of the name itself (e.g. an input literally named "user.id"),
// not a member access.
func IsName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

// UnescapeJSONPointer decodes the RFC 6901 escape sequences inside a
// pointer segment: ~1 is '/', ~0 is '~'. ~1 must be decoded first so
// that ~01 yields the literal ~1.
func UnescapeJSONPointer(seg string) string {
	return strings.ReplaceAll(strings.ReplaceAll(seg, "~1", "/"), "~0", "~")
}

func splitStepOutput(rest string) (step, out string, ok bool) {
	const sep = ".outputs."
	idx := strings.Index(rest, sep)
	if idx < 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+len(sep):], true
}

// embeddedRe matches the spec's embedded form: a runtime expression
// wrapped in {} curly braces inside a string value.
var embeddedRe = regexp.MustCompile(`\{(\$[^{}]+)\}`)

// Refs returns the runtime expressions referenced by s: the whole trimmed
// string when it is itself an expression, otherwise the inner $expr of
// each embedded {$expr} occurrence. A plain literal yields nothing.
func Refs(s string) []string {
	if IsRuntimeExpression(s) {
		return []string{strings.TrimSpace(s)}
	}
	matches := embeddedRe.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// UnsupportedInline returns the runtime expressions among the given
// values that a generator cannot translate inline, deduplicated in
// order. translate is the generator's inline translator: an expression
// is unsupported when translating it returns it unchanged. Callers pass
// the values a step emits inline (parameter values and the request body
// after replacements) so the result matches what is actually serialised.
func UnsupportedInline(values []any, translate func(string) string) []string {
	var out []string
	seen := make(map[string]bool)
	for _, v := range values {
		for _, r := range CollectRefs(v) {
			if seen[r] || translate(r) != r {
				continue
			}
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// CollectRefs walks a JSON-like value (string, map, slice) and returns
// every runtime expression it references, deduplicated and in
// deterministic order (map keys are visited sorted). It lets a generator
// scan a step's parameters and request body for expressions to translate
// or flag, without depending on map iteration order.
func CollectRefs(v any) []string {
	var out []string
	seen := make(map[string]bool)
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			for _, r := range Refs(t) {
				if !seen[r] {
					seen[r] = true
					out = append(out, r)
				}
			}
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(t[k])
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}
