package heredocsyntax

import "testing"

func TestParseHeadersPreservesLiteralDelimiters(t *testing.T) {
	for _, tc := range []struct{ command, delimiter string }{
		{`cat <<EOF`, "EOF"}, {`cat <<E\OF`, "EOF"},
		{`cat <<'E\OF'`, `E\OF`}, {`cat <<"E\OF"`, `E\OF`},
		{`cat <<$'E\x4fF'`, "EOF"}, {`cat <<$(printf x)`, "$(printf x)"},
		{`cat <<$((1+2))`, "$((1+2))"}, {`cat <<"$HOME"`, "$HOME"},
		{`fi; cat <<EOF`, "EOF"}, {`done <<EOF`, "EOF"},
		{`foo) cat <<EOF`, "EOF"}, {`(foo) cat <<EOF`, "EOF"},
		{`cat << <(id)`, "<(id)"}, {`cat << >(id)`, ">(id)"},
		{`cat <<$HOME # $(`, "$HOME"},
		{`cat <<"E\$HOME"`, `E$HOME`},
	} {
		t.Run(tc.command, func(t *testing.T) {
			got := Parse(tc.command, 8192)
			if len(got) != 1 || got[0].Delimiter != tc.delimiter {
				t.Fatalf("got %#v, want delimiter %q", got, tc.delimiter)
			}
		})
	}
}

func TestParseHeadersDoesNotInventRedirections(t *testing.T) {
	for _, command := range []string{`echo ok`, `echo '<<EOF'`, `echo $((1<<2))`, `cat <<< "$value"`, `cat <<`, `cat <<$(`} {
		if got := Parse(command, 8192); len(got) != 0 {
			t.Fatalf("invented redirection in %q: %#v", command, got)
		}
	}
	got := Parse(`cat <<-EOF <<NEXT`, 8192)
	if len(got) != 2 || !got[0].StripTabs || got[1].StripTabs || got[1].Delimiter != "NEXT" {
		t.Fatalf("lost ordered document metadata: %#v", got)
	}
}

func TestLiteralMarkersDoNotCollideAfterQuoteRemoval(t *testing.T) {
	for _, command := range []string{
		`cat <<$HOME <<__AIP_HEREDOC_'0'__`,
		`cat <<$HOME <<__AIP_HEREDOC_\0__`,
		`cat <<$HOME <<$'__AIP_HEREDOC_\x30__'`,
	} {
		got := Parse(command, 8192)
		if len(got) != 2 || got[0].Delimiter != "$HOME" || got[1].Delimiter != "__AIP_HEREDOC_0__" {
			t.Fatalf("marker changed literal identity: %#v", got)
		}
	}
	got := Parse(`cat <<${__AIP_HEREDOC_1__} <<$HOME`, 8192)
	if len(got) != 2 || got[0].Delimiter != "${__AIP_HEREDOC_1__}" || got[1].Delimiter != "$HOME" {
		t.Fatalf("marker collided with another original literal: %#v", got)
	}
}
