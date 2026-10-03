package streamoutput

import (
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

func TestStreamsDoNotCombineOutputKindsOrEmitAfterClose(t *testing.T) {
	var streams Streams
	if streams.Write(0, "\xe2", nil) != "" {
		t.Fatal("partial stdout emitted")
	}
	if streams.Write(1, "stderr", nil) != "stderr" {
		t.Fatal("stderr consumed stdout carry")
	}
	if streams.Write(0, "\x82\xac", nil) != "\u20ac" {
		t.Fatal("stdout carry lost")
	}
	streams.Write(1, "\xf0\x9f", nil)
	if output := streams.Close([2]*sessionenv.Redactor{}); output != [2]string{"", "\ufffd\ufffd"} {
		t.Fatalf("EOF=%q", output)
	}
	if streams.Write(0, "late", nil) != "" || streams.Close([2]*sessionenv.Redactor{}) != [2]string{} {
		t.Fatal("closed streams retained or emitted data")
	}
}

func TestStreamsRedactExactUnicodeBeforeTranscodingAndFinalizePrefixes(t *testing.T) {
	secret := "\u20ac-secret"
	for _, complete := range []bool{true, false} {
		redactor, err := sessionenv.NewRedactor([][]byte{[]byte(secret)})
		if err != nil {
			t.Fatal(err)
		}
		var streams Streams
		var output strings.Builder
		text := "visible " + secret
		if !complete {
			text = text[:len(text)-2]
		}
		for _, value := range []byte(text) {
			output.WriteString(streams.Write(0, string([]byte{value}), redactor))
		}
		last := streams.Close([2]*sessionenv.Redactor{redactor, nil})
		output.WriteString(last[0])
		if output.String() != "visible [REDACTED VAULT VALUE]" || last[1] != "" {
			t.Fatalf("exact secret or prefix exposed: %q", output.String())
		}
	}
}
