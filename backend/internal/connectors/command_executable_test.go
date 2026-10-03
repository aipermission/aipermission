package connectors

import (
	"strings"
	"testing"
)

func TestCommandExecutableRequiresDeclaredNameOrExactAbsoluteWrapper(t *testing.T) {
	for _, standard := range []string{"docker", "kubectl"} {
		for _, value := range []string{"", standard, " " + standard + " ", "/usr/local/bin/" + standard + "-wrapper"} {
			got, err := NormalizeCommandExecutable(value, standard)
			want := strings.TrimSpace(value)
			if want == "" {
				want = standard
			}
			if err != nil || got != want {
				t.Fatalf("executable identity = %q %v; want %q", got, err, want)
			}
		}
		for _, value := range []string{"other-binary", "./" + standard, "../" + standard, "/usr/local/../bin/" + standard, "/usr/local/..", "/tmp/wrapper args", standard + ";id", standard + "$(id)", standard + "\n--version", "/", "/" + strings.Repeat("a", 1024)} {
			if got, err := NormalizeCommandExecutable(value, standard); err == nil || got != "" {
				t.Fatalf("unsafe executable accepted: %q %v", got, err)
			}
		}
	}
	if _, err := NormalizeCommandExecutable("", "bad;command"); err == nil {
		t.Fatal("unsafe standard name accepted")
	}
}
