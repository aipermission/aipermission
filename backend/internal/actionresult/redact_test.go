package actionresult

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactTraversesCanonicalTypedResult(t *testing.T) {
	canonical, err := Canonicalize(map[string]any{
		"events": []typedResultItem{{
			Message: "Bearer raw-token",
			Labels:  map[string]string{"password": "secret", "safe": "value"},
		}},
	}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	redacted, err := Redact(canonical, RedactionOptions{
		SensitiveField: func(key string) bool { return key == "password" },
		RedactText: func(value string) string {
			return strings.ReplaceAll(value, "raw-token", "[REDACTED]")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := jsonText(redacted)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "raw-token") || strings.Contains(text, "secret") {
		t.Fatalf("redacted output leaked a secret: %s", text)
	}
	if !strings.Contains(text, "[REDACTED]") || !strings.Contains(text, "value") {
		t.Fatalf("redacted output = %s", text)
	}
}

func TestCanonicalizeAndRedactTraversesCustomJSONMarshaler(t *testing.T) {
	redacted, err := CanonicalizeAndRedact(customResultValue{}, DefaultLimits(), RedactionOptions{
		SensitiveField: func(key string) bool { return key == "password" },
		RedactText: func(value string) string {
			return strings.ReplaceAll(value, "custom-token", "[REDACTED]")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text, err := jsonText(redacted)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "custom-token") || strings.Contains(text, "secret") {
		t.Fatalf("redacted custom output leaked a secret: %s", text)
	}
}

func TestRedactPreservesDeclaredTemporaryCapabilities(t *testing.T) {
	signedURL := "https://example.test/file?X-Amz-Signature=signature"
	canonical, err := Canonicalize(map[string]any{
		"url":    signedURL,
		"urls":   []string{signedURL},
		"nested": map[string]any{"token": "secret"},
	}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	redacted, err := Redact(canonical, RedactionOptions{
		SensitiveField:           func(key string) bool { return key == "token" },
		TemporaryCapabilityField: func(key string) bool { return key == "url" || key == "urls" },
		RedactText:               func(string) string { return "[BASIC]" },
		RedactCapability:         func(value string) string { return value },
	})
	if err != nil {
		t.Fatal(err)
	}
	root := redacted.(map[string]any)
	if root["url"] != signedURL {
		t.Fatalf("url = %#v", root["url"])
	}
	urls := root["urls"].([]any)
	if len(urls) != 1 || urls[0] != signedURL {
		t.Fatalf("urls = %#v", urls)
	}
	if root["nested"].(map[string]any)["token"] != "[REDACTED]" {
		t.Fatalf("nested = %#v", root["nested"])
	}
}

func TestCanonicalizeAndRedactRevalidatesFinalProjection(t *testing.T) {
	_, err := CanonicalizeAndRedact("x", Limits{
		EncodedBytes: 8,
		Depth:        4,
		Nodes:        4,
		StringBytes:  32,
	}, RedactionOptions{
		RedactText: func(string) string { return "replacement-is-too-large" },
	})
	if err == nil {
		t.Fatal("expected final projection limit error")
	}
}

func jsonText(value any) (string, error) {
	encoded, err := json.Marshal(value)
	return string(encoded), err
}
