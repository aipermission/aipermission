package actionresult

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	MaxEncodedBytes = 4 << 20
	MaxDepth        = 32
	MaxNodes        = 100_000
	MaxStringBytes  = 1 << 20
)

var (
	ErrInvalidValue  = errors.New("invalid canonical JSON value")
	ErrInvalidOutput = ErrInvalidValue
)

// Limits bounds the JSON value accepted from a connector before it reaches
// redaction, persistence, or an external response.
type Limits struct {
	EncodedBytes int
	Depth        int
	Nodes        int
	StringBytes  int
}

func DefaultLimits() Limits {
	return Limits{
		EncodedBytes: MaxEncodedBytes,
		Depth:        MaxDepth,
		Nodes:        MaxNodes,
		StringBytes:  MaxStringBytes,
	}
}

// Canonicalize converts any JSON-serializable Go value into the small set of
// types produced by json.Decoder with UseNumber. This prevents typed structs,
// slices, maps, and custom marshalers from bypassing later traversal.
func Canonicalize(value any, limits Limits) (any, error) {
	limits = normalizedLimits(limits)
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: value is not JSON serializable", ErrInvalidValue)
	}
	if len(encoded) > limits.EncodedBytes {
		return nil, fmt.Errorf("%w: encoded value exceeds %d bytes", ErrInvalidValue, limits.EncodedBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var canonical any
	if err := decoder.Decode(&canonical); err != nil {
		return nil, fmt.Errorf("%w: decode canonical value", ErrInvalidValue)
	}
	nodes := 0
	if err := validateCanonical(canonical, 1, &nodes, limits); err != nil {
		return nil, err
	}
	return canonical, nil
}

func normalizedLimits(limits Limits) Limits {
	defaults := DefaultLimits()
	if limits.EncodedBytes < 1 {
		limits.EncodedBytes = defaults.EncodedBytes
	}
	if limits.Depth < 1 {
		limits.Depth = defaults.Depth
	}
	if limits.Nodes < 1 {
		limits.Nodes = defaults.Nodes
	}
	if limits.StringBytes < 1 {
		limits.StringBytes = defaults.StringBytes
	}
	return limits
}

func validateCanonical(value any, depth int, nodes *int, limits Limits) error {
	if depth > limits.Depth {
		return fmt.Errorf("%w: value exceeds maximum depth %d", ErrInvalidValue, limits.Depth)
	}
	(*nodes)++
	if *nodes > limits.Nodes {
		return fmt.Errorf("%w: value exceeds maximum node count %d", ErrInvalidValue, limits.Nodes)
	}

	switch typed := value.(type) {
	case nil, bool, json.Number:
		return nil
	case string:
		if len(typed) > limits.StringBytes {
			return fmt.Errorf("%w: value contains a string larger than %d bytes", ErrInvalidValue, limits.StringBytes)
		}
		return nil
	case []any:
		for _, item := range typed {
			if err := validateCanonical(item, depth+1, nodes, limits); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for key, item := range typed {
			if len(key) > limits.StringBytes {
				return fmt.Errorf("%w: value contains an object key larger than %d bytes", ErrInvalidValue, limits.StringBytes)
			}
			if err := validateCanonical(item, depth+1, nodes, limits); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: canonical value contains an unsupported type", ErrInvalidValue)
	}
}
