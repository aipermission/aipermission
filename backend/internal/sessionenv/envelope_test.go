package sessionenv

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEnvelopeRejectsUnsafeNamesAndValues(t *testing.T) {
	for _, name := range []string{"PATH", "LD_PRELOAD", "PROMPT_COMMAND", "lowercase", "1TOKEN"} {
		if _, err := NewEnvelope([]EntryInput{{Name: name, Value: []byte("long-enough-secret")}}); err == nil {
			t.Fatalf("expected %q to fail", name)
		}
	}
	if _, err := NewEnvelope([]EntryInput{{Name: "SAFE_TOKEN", Value: []byte("bad\x00value")}}); err == nil {
		t.Fatal("expected NUL value to fail")
	}
	if _, err := NewEnvelope([]EntryInput{{Name: "SAFE_TOKEN", Value: []byte("too-short")}}); err == nil {
		t.Fatal("expected short value to be rejected for injection")
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

func TestRedactorFailsClosedForPartialSecretPrefix(t *testing.T) {
	redactor, err := NewRedactor([][]byte{[]byte("secret")})
	if err != nil {
		t.Fatal(err)
	}
	got := string(redactor.Write([]byte("sec"))) + string(redactor.Close())
	if got != "[REDACTED VAULT VALUE]" {
		t.Fatalf("partial secret prefix output = %q", got)
	}
}

func TestRedactorRedactDoesNotChangeStreamingState(t *testing.T) {
	redactor, err := NewRedactor([][]byte{[]byte("secret-value")})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(redactor.Redact([]byte("command secret-value"))); got != "command [REDACTED VAULT VALUE]" {
		t.Fatalf("one-shot output = %q", got)
	}
	got := string(redactor.Write([]byte("secret-"))) +
		string(redactor.Write([]byte("value"))) +
		string(redactor.Close())
	if got != "[REDACTED VAULT VALUE]" {
		t.Fatalf("streaming output after one-shot redaction = %q", got)
	}
}

func TestRedactorCoversEveryTwoChunkSplit(t *testing.T) {
	secret := []byte("secret-value-across-every-boundary")
	input := append([]byte("before "), secret...)
	input = append(input, []byte(" after")...)
	for split := 0; split <= len(input); split++ {
		redactor, err := NewRedactor([][]byte{secret})
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		output.Write(redactor.Write(input[:split]))
		output.Write(redactor.Write(input[split:]))
		output.Write(redactor.Close())
		if got := output.String(); got != "before [REDACTED VAULT VALUE] after" {
			t.Fatalf("split %d output = %q", split, got)
		}
	}
}

func TestRedactorCoversTerminalNewlineTransformations(t *testing.T) {
	secret := []byte("first line\nsecond line")
	for _, transformed := range [][]byte{
		[]byte("first line\r\nsecond line"),
		[]byte("first line\rsecond line"),
	} {
		redactor, err := NewRedactor([][]byte{secret})
		if err != nil {
			t.Fatal(err)
		}
		got := string(redactor.Write(transformed)) + string(redactor.Close())
		if got != "[REDACTED VAULT VALUE]" {
			t.Fatalf("terminal-transformed output = %q", got)
		}
	}
}

func TestRedactorDropsWritesAfterClose(t *testing.T) {
	redactor, err := NewRedactor([][]byte{[]byte("secret-value")})
	if err != nil {
		t.Fatal(err)
	}
	_ = redactor.Close()
	if output := redactor.Write([]byte("ordinary output")); len(output) != 0 {
		t.Fatalf("write after close = %q", output)
	}
}
