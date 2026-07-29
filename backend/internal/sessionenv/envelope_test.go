package sessionenv

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEnvelopeRejectsUnsafeNamesAndValues(t *testing.T) {
	for _, name := range []string{"PATH", "LD_PRELOAD", "PROMPT_COMMAND", "lowercase", "1TOKEN"} {
		if _, err := NewEnvelope([]EntryInput{{Name: name, Value: []byte("secret")}}); err == nil {
			t.Fatalf("expected %q to fail", name)
		}
	}
	if _, err := NewEnvelope([]EntryInput{{Name: "SAFE_TOKEN", Value: []byte("bad\x00value")}}); err == nil {
		t.Fatal("expected NUL value to fail")
	}
}

func TestEnvelopeHasNoJSONSecretRepresentationAndDestroysValues(t *testing.T) {
	envelope, err := NewEnvelope([]EntryInput{{Name: "SAFE_TOKEN", Value: []byte("top-secret-value")}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "top-secret-value") {
		t.Fatalf("JSON exposed secret: %s", encoded)
	}
	envelope.Destroy()
	if err := envelope.WithEntries(func([]EntryView) error { return nil }); err != ErrDestroyed {
		t.Fatalf("destroyed envelope error = %v", err)
	}
}

func TestRedactorCoversChunkBoundariesAndPrefixPatterns(t *testing.T) {
	redactor, err := NewRedactor([][]byte{[]byte("secret"), []byte("secret-longer"), []byte("other")})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	for _, chunk := range [][]byte{
		[]byte("before sec"),
		[]byte("ret-long"),
		[]byte("er and oth"),
		[]byte("er after"),
	} {
		output.Write(redactor.Write(chunk))
	}
	output.Write(redactor.Close())
	got := output.String()
	if strings.Contains(got, "secret") || strings.Contains(got, "other") {
		t.Fatalf("redactor leaked exact value: %q", got)
	}
	if got != "before [REDACTED VAULT VALUE] and [REDACTED VAULT VALUE] after" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestRedactorFlushesOrdinaryPartialPrefixes(t *testing.T) {
	redactor, err := NewRedactor([][]byte{[]byte("secret")})
	if err != nil {
		t.Fatal(err)
	}
	got := string(redactor.Write([]byte("sec"))) + string(redactor.Close())
	if got != "sec" {
		t.Fatalf("partial ordinary output = %q", got)
	}
}
