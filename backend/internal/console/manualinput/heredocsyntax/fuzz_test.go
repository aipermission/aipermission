package heredocsyntax

import "testing"

func FuzzParseHeadersBounded(f *testing.F) {
	for _, seed := range []string{`cat <<EOF`, `foo) cat <<$(id)`, `cat << <(id)`, `cat <<$'E\x4fF'`, `echo $((1<<2))`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 1024 {
			return
		}
		documents := Parse(source, 1024)
		if len(documents) > len(source) {
			t.Fatal("document count exceeds the bounded input")
		}
		for _, document := range documents {
			if len(document.Delimiter) > len(source)*4 {
				t.Fatal("delimiter exceeds the bounded input representation")
			}
		}
	})
}
