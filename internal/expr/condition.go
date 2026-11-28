package expr

import (
	"regexp"
	"strings"
)

// Subject is what the left-hand side of a simple condition reads from
// the response.
type Subject int

const (
	// SubjectStatus is $statusCode.
	SubjectStatus Subject = iota + 1
	// SubjectBody is $response.body, whole or with a #/<json-pointer>.
	SubjectBody
	// SubjectHeader is $response.header.<name>.
	SubjectHeader
)

// LiteralKind classifies the right-hand side of a simple condition.
type LiteralKind int

const (
	LiteralString LiteralKind = iota + 1
	LiteralNumber
	LiteralBool
	LiteralNull
)

// Condition is a simple-condition criterion of the form
// "<subject> <op> <literal>", the subset both test generators assert on.
type Condition struct {
	Subject    Subject
	Pointer    string // JSON-pointer body after "#/" for SubjectBody, "" for the whole body
	HeaderName string // for SubjectHeader
	Op         string // ==, !=, <, <=, >, >=
	Kind       LiteralKind
	Value      string // unquoted string, number text, "true"/"false", or "null"
}

var (
	conditionRe = regexp.MustCompile(`^(\$[^\s<>=!]+)\s*(==|!=|<=|>=|<|>)\s*(.+)$`)
	numberRe    = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
	headerRe    = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
)

// ParseCondition recognises a simple condition comparing $statusCode,
// $response.body[#/pointer] or $response.header.<name> with a literal.
// Strings use single quotes, a doubled quote escaping one, per the spec; double
// quotes without an inner '"' are tolerated because authors write them.
// Anything else (&&, ||, !, grouping, an expression on the right, a
// literal on the left) returns ok=false so callers keep it a comment.
func ParseCondition(cond string) (Condition, bool) {
	m := conditionRe.FindStringSubmatch(strings.TrimSpace(cond))
	if m == nil {
		return Condition{}, false
	}
	c := Condition{Op: m[2]}
	if !parseSubject(m[1], &c) || !parseLiteral(strings.TrimSpace(m[3]), &c) {
		return Condition{}, false
	}
	if c.Subject == SubjectStatus && c.Kind != LiteralNumber {
		return Condition{}, false
	}
	return c, true
}

func parseSubject(s string, c *Condition) bool {
	switch e := Parse(s); {
	case e.Kind == KindStatusCode:
		c.Subject = SubjectStatus
	case e.Kind == KindResponseBody:
		c.Subject, c.Pointer = SubjectBody, e.Pointer
	case strings.HasPrefix(s, "$response.header."):
		name := strings.TrimPrefix(s, "$response.header.")
		if !headerRe.MatchString(name) {
			return false
		}
		c.Subject, c.HeaderName = SubjectHeader, name
	default:
		return false
	}
	return true
}

func parseLiteral(s string, c *Condition) bool {
	switch {
	case s == "true" || s == "false":
		c.Kind, c.Value = LiteralBool, s
	case s == "null":
		c.Kind, c.Value = LiteralNull, s
	case numberRe.MatchString(s):
		c.Kind, c.Value = LiteralNumber, s
	case len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'':
		inner := s[1 : len(s)-1]
		// A lone quote inside means the literal ended early and something
		// follows it (e.g. "'a' && ..."), so this is not one literal.
		if strings.Contains(strings.ReplaceAll(inner, "''", ""), "'") {
			return false
		}
		c.Kind, c.Value = LiteralString, strings.ReplaceAll(inner, "''", "'")
	case len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' && !strings.Contains(s[1:len(s)-1], `"`):
		c.Kind, c.Value = LiteralString, s[1:len(s)-1]
	default:
		return false
	}
	return true
}
