package jsonidentity

import (
	"encoding/json"
	"testing"
)

func TestValidateRejectsLossyUnicodeAndInvalidJSON(t *testing.T) {
	for _, data := range []string{
		`"\ud800"`, `"\udfff"`, `"\ud800x"`, `"\ud800\u0041"`, `"\ud800\ud800"`,
		`{"\ud800":"key"}`, `{"nested":[{"column":"\ud800"}]}`, `"` + string([]byte{0xff}) + `"`,
		`"\uzzzz"`, `"\u00"`, `"\`, `{} {}`, ``,
	} {
		if Validate([]byte(data)) == nil {
			t.Fatalf("lossy or invalid JSON accepted: %q", data)
		}
	}
}

func TestValidatePreservesValidUnicodeAndEscapeParity(t *testing.T) {
	for _, data := range []string{
		`"\ud83d\ude80"`, `"\ufffd"`, `"` + "\ufffd" + `"`, `"\\ud800"`,
		`"\\\ud83d\ude80"`, `"\\\\ud800"`, `"\"\\ud800"`, `"\u0000"`, `{"name":"a","n":9007199254740993}`, `null`,
	} {
		if err := Validate([]byte(data)); err != nil {
			t.Fatalf("valid JSON rejected: %q: %v", data, err)
		}
	}
}

func FuzzValidateAcceptsEncoderOutputWithoutPanicking(f *testing.F) {
	for _, seed := range []string{`{}`, `"\ud800"`, `"\ud83d\ude80"`, `"\\ud800"`, `"\`, "\xff"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		err := Validate([]byte(text))
		if err == nil && !json.Valid([]byte(text)) {
			t.Fatal("accepted invalid JSON")
		}
		encoded, marshalErr := json.Marshal(text)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if err := Validate(encoded); err != nil {
			t.Fatalf("rejected standard encoder output: %v", err)
		}
	})
}
