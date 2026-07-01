// Package payload applies Arazzo Payload Replacement Objects to a request
// body payload. A replacement sets the value at a JSON-pointer target
// (RFC 6901) before the body is serialised, so the generated test carries
// the injected value rather than dropping it silently.
package payload

import (
	"strconv"
	"strings"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
)

// Apply returns a copy of root with each replacement's value set at its
// JSON-pointer target. The input is never mutated (the model payload is
// shared with the renderer and the other generator). Targets that do not
// resolve against the payload are returned, in order, so the caller can
// surface them instead of failing silently.
func Apply(root any, repls []model.Replacement) (any, []string) {
	if len(repls) == 0 {
		return root, nil
	}
	out := deepCopy(root)
	var unresolved []string
	for _, r := range repls {
		next, ok := setAtPointer(out, r.Target, deepCopy(r.Value))
		if !ok {
			unresolved = append(unresolved, r.Target)
			continue
		}
		out = next
	}
	return out, unresolved
}

// setAtPointer sets value at the RFC 6901 pointer and returns the
// (possibly new) root. An empty pointer replaces the whole document. It
// reports ok=false when an intermediate token does not address an
// existing container.
func setAtPointer(root any, pointer string, value any) (any, bool) {
	if pointer == "" {
		return value, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return root, false
	}
	tokens := strings.Split(pointer[1:], "/")
	for i := range tokens {
		tokens[i] = unescape(tokens[i])
	}
	return setTokens(root, tokens, value)
}

func setTokens(node any, tokens []string, value any) (any, bool) {
	token := tokens[0]
	last := len(tokens) == 1
	switch n := node.(type) {
	case map[string]any:
		if last {
			n[token] = value
			return n, true
		}
		child, ok := n[token]
		if !ok {
			return node, false
		}
		newChild, ok := setTokens(child, tokens[1:], value)
		if !ok {
			return node, false
		}
		n[token] = newChild
		return n, true
	case []any:
		idx, err := strconv.Atoi(token)
		// RFC 6901 array indices are canonical decimals: reject a leading
		// '+' or leading zeros (Atoi accepts both) so "/items/01" does not
		// silently address index 1.
		if err != nil || idx < 0 || idx >= len(n) || token != strconv.Itoa(idx) {
			return node, false
		}
		if last {
			n[idx] = value
			return n, true
		}
		newChild, ok := setTokens(n[idx], tokens[1:], value)
		if !ok {
			return node, false
		}
		n[idx] = newChild
		return n, true
	default:
		return node, false
	}
}

// unescape decodes the RFC 6901 token escapes: ~1 is '/', ~0 is '~'. ~1
// must be decoded first so ~01 yields the literal ~1.
func unescape(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
}

func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = deepCopy(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = deepCopy(val)
		}
		return out
	default:
		return v
	}
}
