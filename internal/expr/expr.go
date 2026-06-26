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

import "strings"

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
	// KindResponseBody is $response.body, with a #/<pointer> suffix.
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
// the original string preserved in Raw.
func Parse(s string) Expr {
	e := strings.TrimSpace(s)
	switch {
	case e == "$statusCode":
		return Expr{Kind: KindStatusCode, Raw: s}
	case strings.HasPrefix(e, "$response.body#/"):
		return Expr{
			Kind:       KindResponseBody,
			Pointer:    strings.TrimPrefix(e, "$response.body#/"),
			HasPointer: true,
			Raw:        s,
		}
	case strings.HasPrefix(e, "$inputs."):
		if name := strings.TrimPrefix(e, "$inputs."); IsName(name) {
			return Expr{Kind: KindInput, Name: name, Raw: s}
		}
	case strings.HasPrefix(e, "$steps."):
		rest := strings.TrimPrefix(e, "$steps.")
		if step, out, ok := splitStepOutput(rest); ok && IsName(step) && IsName(out) {
			return Expr{Kind: KindStepOutput, Name: step, OutputName: out, Raw: s}
		}
	}
	return Expr{Kind: KindUnknown, Raw: s}
}

// IsRuntimeExpression reports whether s is (the start of) an Arazzo
// runtime expression: a "$"-prefixed token. It does not validate the
// form, only the sigil, so callers can distinguish an unsupported
// expression from plain literal data.
func IsRuntimeExpression(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "$")
}

// IsName reports whether s is a plain Arazzo name the generators can map
// to a variable: letters, digits, '_' or '-'.
func IsName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func splitStepOutput(rest string) (step, out string, ok bool) {
	const sep = ".outputs."
	idx := strings.Index(rest, sep)
	if idx < 0 {
		return "", "", false
	}
	return rest[:idx], rest[idx+len(sep):], true
}
