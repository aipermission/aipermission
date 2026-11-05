// Package jsonshape measures the shared structural limits of canonical JSON.
package jsonshape

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const (
	MaxDepth       = 32
	MaxNodes       = 100000
	MaxStringBytes = 1 << 20
)

type Limits struct{ Depth, Nodes, StringBytes int }

func Measure(value any, limits Limits) (int, error) {
	nodes := 0
	err := validate(value, 1, &nodes, limits)
	return nodes, err
}

// MeasureJSON accepts encoded JSON, including typed Go values after Marshal.
// Callers must enforce their byte limit before allocating the decoded tree.
func MeasureJSON(encoded []byte, limits Limits) (int, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return 0, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return 0, fmt.Errorf("expected exactly one JSON value")
	}
	return Measure(value, limits)
}

func validate(value any, depth int, nodes *int, limits Limits) error {
	if depth > limits.Depth {
		return fmt.Errorf("value exceeds maximum depth %d", limits.Depth)
	}
	(*nodes)++
	if *nodes > limits.Nodes {
		return fmt.Errorf("value exceeds maximum node count %d", limits.Nodes)
	}
	switch typed := value.(type) {
	case nil, bool, json.Number:
		return nil
	case string:
		if len(typed) > limits.StringBytes {
			return fmt.Errorf("value contains a string larger than %d bytes", limits.StringBytes)
		}
	case []any:
		for _, item := range typed {
			if err := validate(item, depth+1, nodes, limits); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, item := range typed {
			if len(key) > limits.StringBytes {
				return fmt.Errorf("value contains an object key larger than %d bytes", limits.StringBytes)
			}
			if err := validate(item, depth+1, nodes, limits); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("canonical value contains an unsupported type")
	}
	return nil
}
