package manualinput

import (
	"strings"
	"testing"
)

func TestNonHeredocOperatorsDoNotSuppressFollowingHistory(t *testing.T) {
	for _, command := range []string{`grep x <<< "$v"`, `echo $((1<<2))`, `((value <<= 1))`, `echo "a<<b"`, `echo 'a<<b'`, `echo ok # <<EOF`, `foo) echo '<<EOF'`, `(foo) echo $((1<<2))`} {
		t.Run(command, func(t *testing.T) {
			var capture Capture
			capture.Consume(command + "\n")
			for _, next := range []string{"hostname", "pwd"} {
				got := capture.Consume(next + "\n")
				if len(got) != 1 || got[0].Command != next || !got[0].TrackOutput {
					t.Fatalf("history after %q: %#v", command, got)
				}
			}
		})
	}
}

func TestHeredocBodyStaysUntrackedUntilExactDelimiter(t *testing.T) {
	for _, command := range []string{`cat <<EOF`, `cat <<'EOF'`, `cat <<E"OF"`, `cat <<-EOF`} {
		t.Run(command, func(t *testing.T) {
			var capture Capture
			got := capture.Consume(command + "\n")
			if len(got) != 1 || got[0].TrackOutput || got[0].TrackingReason != "multiline_or_heredoc" {
				t.Fatalf("heredoc observation: %#v", got)
			}
			if got := capture.Consume("secret body\n EOF \n"); len(got) != 0 {
				t.Fatalf("body became command history: %#v", got)
			}
			capture.Consume("EOF\n")
			if got := capture.Consume("hostname\n"); len(got) != 1 || got[0].Command != "hostname" {
				t.Fatalf("history did not resume: %#v", got)
			}
		})
	}
}

func TestHeredocDelimitersAreLiteralAndOrdered(t *testing.T) {
	for _, tc := range []struct{ command, body string }{
		{`cat <<EOF <<NEXT`, "body\nEOF\nother\nNEXT\n"},
		{`cat <<-EOF`, "body\n\tEOF\n"},
		{`cat <<''`, "body\n\n"},
		{`cat <<$HOME`, "body\n$HOME\n"},
		{`cat <<E\OF`, "body\nEOF\n"},
		{`if true; then cat <<EOF`, "secret-body\nEOF\n"},
		{`for item in one; do cat <<EOF`, "secret-body\nEOF\n"},
		{strings.Repeat("if true; then ", 20) + "cat <<EOF", "secret-body\nEOF\n"},
		{`fi; cat <<EOF`, "secret-body\nEOF\n"},
		{`done <<EOF`, "secret-body\nEOF\n"},
		{`esac; cat <<EOF`, "secret-body\nEOF\n"},
		{`} <<EOF`, "secret-body\nEOF\n"},
		{`) <<EOF`, "secret-body\nEOF\n"},
		{`elif true; then cat <<EOF`, "secret-body\nEOF\n"},
		{`;; esac; cat <<EOF`, "secret-body\nEOF\n"},
		{`foo) cat <<EOF`, "secret-body\nEOF\n"},
		{`(foo) cat <<EOF`, "secret-body\nEOF\n"},
		{`foo|bar) cat <<EOF`, "secret-body\nEOF\n"},
		{`cat <<$'E\x4fF'`, "secret-body\nEOF\n"},
		{`cat <<$(id)`, "secret-body\n$(id)\n"},
		{`cat <<$((1+2))`, "secret-body\n$((1+2))\n"},
		{`cat <<$(printf x)`, "secret-body\n$(printf x)\n"},
		{`cat <<"$(id)"`, "secret-body\n$(id)\n"},
		{`cat <<E$(id)`, "secret-body\nE$(id)\n"},
		{"cat <<`id`", "secret-body\n`id`\n"},
		{`cat <<$HOME <<$(id)`, "secret-body\n$HOME\nother-body\n$(id)\n"},
		{`cat <<${__AIP_HEREDOC_1__} <<$HOME`, "secret-body\n${__AIP_HEREDOC_1__}\nother-body\n$HOME\n"},
		{`cat << <(id)`, "secret-body\n<(id)\n"},
		{`cat << >(id)`, "secret-body\n>(id)\n"},
		{`cat <<$HOME # $(`, "secret-body\n$HOME\n"},
		{`cat <<$HOME <<__AIP_HEREDOC_'0'__`, "secret-body\n$HOME\nother-body\n__AIP_HEREDOC_0__\n"},
		{`cat <<$HOME <<__AIP_HEREDOC_\0__`, "secret-body\n$HOME\nother-body\n__AIP_HEREDOC_0__\n"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			var capture Capture
			capture.Consume(tc.command + "\n")
			if got := capture.Consume(tc.body); len(got) != 0 {
				t.Fatalf("heredoc content entered history: %#v", got)
			}
			if got := capture.Consume("pwd\n"); len(got) != 1 || got[0].Command != "pwd" {
				t.Fatalf("history did not resume: %#v", got)
			}
		})
	}
}
