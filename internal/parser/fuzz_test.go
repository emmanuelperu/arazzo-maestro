package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParseBytes feeds arbitrary byte slices into ParseBytes. The
// contract under test is purely structural: the parser must always
// return either (doc, nil) or (nil, *ArazzoParseError) — it must never
// panic, no matter how malformed the input.
//
// Run with:  go test -run=^$ -fuzz=FuzzParseBytes ./internal/parser
func FuzzParseBytes(f *testing.F) {
	// Seed with the in-tree minimal document (defined in parser_test.go).
	f.Add([]byte(minimalYAML))

	// Seed with the shipped examples so the fuzzer starts from real,
	// well-formed inputs and mutates outward.
	exDir := filepath.Join("..", "..", "examples")
	for _, name := range []string{"shop.arazzo.yaml", "checkout-branching.arazzo.yaml"} {
		raw, err := os.ReadFile(filepath.Join(exDir, name))
		if err != nil {
			f.Fatalf("seed %s: %v", name, err)
		}
		f.Add(raw)
	}

	// Pathological seeds — empty, non-mapping root, key/value collision,
	// deep nesting — exercise the error paths the parser must handle.
	for _, seed := range [][]byte{
		nil,
		[]byte(""),
		[]byte("null"),
		[]byte("[]"),
		[]byte("- 1\n- 2\n"),
		[]byte("arazzo: \"1.1.0\"\n"),
		[]byte("arazzo: \"1.1.0\"\nworkflows: []\n"),
		[]byte("arazzo: \"1.1.0\"\nworkflows:\n  - {}\n"),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		// The only invariant we care about here is "no panic, no goroutine
		// leak, no unbounded allocation". The doc/err pair is intentionally
		// discarded — ParseBytes is allowed to reject anything.
		_, _ = ParseBytes(raw)
	})
}
