package httpattachment

import (
	"mime"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHeadersRejectFilenameInjectionAndSniffing(t *testing.T) {
	response := httptest.NewRecorder()
	SetHeaders(response, "../report\r\nX-Injected: yes.json", "application/json")

	if got := response.Header().Get("Cache-Control"); got != "no-store, private" {
		t.Fatalf("Cache-Control = %q, want no-store, private", got)
	}
	if got := response.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", got)
	}
	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	disposition, parameters, err := mime.ParseMediaType(response.Header().Get("Content-Disposition"))
	if err != nil {
		t.Fatalf("parse Content-Disposition: %v", err)
	}
	if disposition != "attachment" || parameters["filename"] != "reportX-Injected: yes.json" {
		t.Fatalf("unexpected Content-Disposition: %q %#v", disposition, parameters)
	}
	for name := range response.Header() {
		if strings.EqualFold(name, "X-Injected") {
			t.Fatal("filename created an injected response header")
		}
	}
}

func TestSafeFilenameUsesBoundedBasename(t *testing.T) {
	tests := map[string]string{
		"../nested/report.sql":       "report.sql",
		`..\nested\windows.sql`:      "windows.sql",
		"\x00\r\n":                   "fallback.bin",
		" normal-backup.aipdb ":      "normal-backup.aipdb",
		"folder/control\x00name.zip": "controlname.zip",
		"":                           "fallback.bin",
	}
	for input, want := range tests {
		if got := SafeFilename(input, "fallback.bin"); got != want {
			t.Fatalf("SafeFilename(%q) = %q, want %q", input, got, want)
		}
	}
	longName := strings.Repeat("x", MaxFilenameRunes+20)
	if got := SafeFilename(longName, "fallback.bin"); len([]rune(got)) != MaxFilenameRunes {
		t.Fatalf("long filename length = %d, want %d", len([]rune(got)), MaxFilenameRunes)
	}
	if got := SafeFilename("\x00", "../fallback\r\n.bin"); got != "fallback.bin" {
		t.Fatalf("sanitized fallback = %q, want fallback.bin", got)
	}
}
