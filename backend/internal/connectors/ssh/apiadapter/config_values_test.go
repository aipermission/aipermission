package apiadapter

import (
	"encoding/json"
	"strconv"
	"testing"
)

func TestIntConfigValueBoundsParsedInputToNativeInt(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		fallback int
		want     int
	}{
		{
			name:     "string",
			value:    "2222",
			fallback: 22,
			want:     2222,
		},
		{
			name:     "json number",
			value:    json.Number("5432"),
			fallback: 22,
			want:     5432,
		},
		{
			name:     "overflow string falls back",
			value:    "9223372036854775808",
			fallback: 22,
			want:     22,
		},
		{
			name:     "overflow json number falls back",
			value:    json.Number("9223372036854775808"),
			fallback: 22,
			want:     22,
		},
		{
			name:     "fractional float falls back",
			value:    22.5,
			fallback: 22,
			want:     22,
		},
		{
			name:     "zero falls back",
			value:    0,
			fallback: 22,
			want:     22,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := intConfigValue(map[string]any{"port": tt.value}, "port", tt.fallback)
			if got != tt.want {
				t.Fatalf("intConfigValue() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNativeIntValueRejectsOutOfRangeInt64On32BitPlatforms(t *testing.T) {
	got, ok := nativeIntValue(int64(1234))
	if !ok || got != 1234 {
		t.Fatalf("nativeIntValue(int64(1234)) = (%d, %v), want (1234, true)", got, ok)
	}

	if strconv.IntSize != 32 {
		t.Skip("native int is not 32-bit on this platform")
	}
	if _, ok := nativeIntValue(int64(1 << 40)); ok {
		t.Fatal("nativeIntValue accepted an int64 outside 32-bit int range")
	}
}
