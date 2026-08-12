package actionresult

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

type typedResultItem struct {
	Message string            `json:"message"`
	Labels  map[string]string `json:"labels"`
}

type customResultValue struct{}

func (customResultValue) MarshalJSON() ([]byte, error) {
	return []byte(`{"message":"Bearer custom-token","nested":{"password":"secret"}}`), nil
}

func TestCanonicalizeTypedValues(t *testing.T) {
	input := map[string]any{
		"items": &[]typedResultItem{{Message: "Bearer raw-token", Labels: map[string]string{"password": "secret"}}},
		"count": int64(1),
	}
	canonical, err := Canonicalize(input, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	root, ok := canonical.(map[string]any)
	if !ok {
		t.Fatalf("canonical root type = %T", canonical)
	}
	items, ok := root["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("canonical items = %#v", root["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok || item["message"] != "Bearer raw-token" {
		t.Fatalf("canonical item = %#v", items[0])
	}
	if number, ok := root["count"].(json.Number); !ok || number.String() != "1" {
		t.Fatalf("canonical number = %#v", root["count"])
	}
}

func TestCanonicalizeCustomJSONMarshaler(t *testing.T) {
	canonical, err := Canonicalize(customResultValue{}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	root, ok := canonical.(map[string]any)
	if !ok || root["message"] != "Bearer custom-token" {
		t.Fatalf("canonical custom value = %#v", canonical)
	}
	nested, ok := root["nested"].(map[string]any)
	if !ok || nested["password"] != "secret" {
		t.Fatalf("canonical custom nested value = %#v", root["nested"])
	}
}

func TestCanonicalizeRejectsUnsupportedAndCyclicValues(t *testing.T) {
	if _, err := Canonicalize(func() {}, DefaultLimits()); !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("unsupported value error = %v", err)
	}
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	if _, err := Canonicalize(cyclic, DefaultLimits()); !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("cyclic value error = %v", err)
	}
	if _, err := Canonicalize(math.Inf(1), DefaultLimits()); !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("non-JSON number error = %v", err)
	}
}

func TestCanonicalizeEnforcesLimits(t *testing.T) {
	tests := []struct {
		name   string
		value  any
		limits Limits
	}{
		{name: "bytes", value: "12345", limits: Limits{EncodedBytes: 4, Depth: 10, Nodes: 10, StringBytes: 10}},
		{name: "depth", value: []any{[]any{true}}, limits: Limits{EncodedBytes: 100, Depth: 2, Nodes: 10, StringBytes: 10}},
		{name: "nodes", value: []any{true, false}, limits: Limits{EncodedBytes: 100, Depth: 10, Nodes: 2, StringBytes: 10}},
		{name: "string", value: "12345", limits: Limits{EncodedBytes: 100, Depth: 10, Nodes: 10, StringBytes: 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Canonicalize(tt.value, tt.limits); !errors.Is(err, ErrInvalidOutput) {
				t.Fatalf("limit error = %v", err)
			}
		})
	}
}

func TestCanonicalizeErrorsDoNotIncludeOutputValues(t *testing.T) {
	secret := "private-secret-that-must-not-appear"
	_, err := Canonicalize(strings.Repeat(secret, 4), Limits{EncodedBytes: 16, Depth: 10, Nodes: 10, StringBytes: 1 << 20})
	if err == nil {
		t.Fatal("expected size error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked output value: %v", err)
	}
}
