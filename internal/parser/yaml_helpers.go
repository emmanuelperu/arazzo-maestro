package parser

import (
	"strconv"

	"gopkg.in/yaml.v3"
)

// kv is one key/value pair of a yaml MappingNode, with the key resolved
// to a Go string for ergonomic switch statements.
type kv struct {
	Key   string
	Value *yaml.Node
}

// mappingPairs walks a MappingNode and returns its pairs in declaration
// order. Non-mapping nodes return nil.
func mappingPairs(n *yaml.Node) []kv {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	pairs := make([]kv, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		pairs = append(pairs, kv{
			Key:   n.Content[i].Value,
			Value: n.Content[i+1],
		})
	}
	return pairs
}

// scalarString returns the textual value of a scalar node, or "" for nil
// or non-scalar nodes. Explicit `null` scalars also return "".
func scalarString(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	if n.Tag == "!!null" {
		return ""
	}
	return n.Value
}

// nodeToAny converts a yaml.Node into the plain Go value that yaml.Unmarshal
// into `any` would produce.
func nodeToAny(n *yaml.Node) any {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil
		}
		return nodeToAny(n.Content[0])
	case yaml.ScalarNode:
		return scalarToAny(n)
	case yaml.SequenceNode:
		items := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			items = append(items, nodeToAny(c))
		}
		return items
	case yaml.MappingNode:
		m := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			m[n.Content[i].Value] = nodeToAny(n.Content[i+1])
		}
		return m
	case yaml.AliasNode:
		return nodeToAny(n.Alias)
	}
	return nil
}

func scalarToAny(n *yaml.Node) any {
	switch n.Tag {
	case "!!null":
		return nil
	case "!!bool":
		return n.Value == "true"
	case "!!int":
		if v, err := strconv.ParseInt(n.Value, 10, 64); err == nil {
			return v
		}
		return n.Value
	case "!!float":
		if v, err := strconv.ParseFloat(n.Value, 64); err == nil {
			return v
		}
		return n.Value
	}
	return n.Value
}
