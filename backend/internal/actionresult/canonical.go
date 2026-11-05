package actionresult

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/jsonnumber"
	"github.com/aipermission/aipermission/backend/internal/jsonshape"
)

const (
	MaxEncodedBytes = 4 << 20
	MaxDepth        = jsonshape.MaxDepth
	MaxNodes        = jsonshape.MaxNodes
	MaxStringBytes  = jsonshape.MaxStringBytes
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
// types produced by json.Decoder with UseNumber. Inexact JavaScript numbers
// become strings. Typed/custom values cannot bypass traversal or output limits.
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
	if err := validateCanonical(canonical, limits); err != nil {
		return nil, err
	}
	canonical = jsonnumber.PublicValue(canonical)
	if err := validateCanonical(canonical, limits); err != nil {
		return nil, err
	}
	projected, err := json.Marshal(canonical)
	if err != nil || len(projected) > limits.EncodedBytes {
		return nil, fmt.Errorf("%w: numeric projection exceeds encoded value limit", ErrInvalidValue)
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

func validateCanonical(value any, limits Limits) error {
	_, err := jsonshape.Measure(value, jsonshape.Limits{Depth: limits.Depth, Nodes: limits.Nodes, StringBytes: limits.StringBytes})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidValue, err)
	}
	return nil
}
