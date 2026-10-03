// Package jsonnumber preserves numeric data across JSON/JavaScript boundaries.
package jsonnumber

import (
	"encoding/json"
	"math"
	"math/big"
	"strconv"
	"strings"
)

const maxSafeInteger = 1<<53 - 1

// PublicValue projects a decoded UseNumber JSON tree without rounding numbers.
// Numbers that cannot safely round-trip through JavaScript become exact strings.
// The caller owns JSON validity, traversal and encoded-size limits.
func PublicValue(value any) any {
	switch value := value.(type) {
	case json.Number:
		if safeNumber(value.String()) {
			return value
		}
		return value.String()
	case []any:
		for index, item := range value {
			value[index] = PublicValue(item)
		}
	case map[string]any:
		for key, item := range value {
			value[key] = PublicValue(item)
		}
	}
	return value
}

func safeNumber(text string) bool {
	// Bound rational comparison work; oversized numeric tokens stay exact text.
	if len(text) > 1024 {
		return false
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) || (math.Trunc(value) == value && math.Abs(value) > maxSafeInteger) {
		return false
	}
	if value == 0 {
		if math.Signbit(value) {
			return false
		}
		mantissa, _, _ := strings.Cut(strings.ToLower(text), "e")
		return !strings.ContainsAny(mantissa, "123456789")
	}
	original, ok := new(big.Rat).SetString(text)
	if !ok {
		return false
	}
	roundTrip, ok := new(big.Rat).SetString(strconv.FormatFloat(value, 'g', -1, 64))
	return ok && original.Cmp(roundTrip) == 0
}
