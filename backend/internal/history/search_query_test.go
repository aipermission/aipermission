package history

import (
	"reflect"
	"testing"
)

func TestTermsSanitizeAndBoundInput(t *testing.T) {
	got := Terms(`docker:"ps" OR password=secret; apt-get`, 8)
	want := []string{"docker", "ps", "or", "password", "secret", "apt", "get"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("terms=%v want=%v", got, want)
	}
}

func TestFTSBoundsAndRejectsPunctuationOnlyInput(t *testing.T) {
	if got := FTS("one two three four five six seven eight nine ten"); got != "one two three four five six seven eight" {
		t.Fatalf("bounded query=%q", got)
	}
	if got := FTS(`"":;()--`); got != "" {
		t.Fatalf("punctuation query=%q", got)
	}
}
