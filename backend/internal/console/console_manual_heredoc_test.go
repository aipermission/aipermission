package console

import (
	"strings"
	"testing"
)

func TestManualHeredocAfterClosingConstructNeverPersistsBody(t *testing.T) {
	for _, script := range []string{
		"if true; then\n:\nfi; cat <<EOF\nprivate-body\nEOF\npwd\n",
		"for item in one; do\n:\ndone <<EOF\nprivate-body\nEOF\npwd\n",
		"case foo in\nfoo) cat <<EOF\nprivate-body\nEOF\n;;\nesac\npwd\n",
		"case foo in\n(foo) cat <<EOF\nprivate-body\nEOF\n;;\nesac\npwd\n",
		"cat <<$(id)\nprivate-body\n$(id)\npwd\n",
		"cat <<$((1+2))\nprivate-body\n$((1+2))\npwd\n",
		"cat << <(id)\nprivate-body\n<(id)\npwd\n",
		"cat << >(id)\nprivate-body\n>(id)\npwd\n",
		"cat <<$HOME # $(\nprivate-body\n$HOME\npwd\n",
		"cat <<$HOME <<__AIP_HEREDOC_'0'__\nprivate-body\n$HOME\nprivate-body\n__AIP_HEREDOC_0__\npwd\n",
	} {
		for _, pasted := range []bool{false, true} {
			t.Run(script+map[bool]string{true: "paste", false: "line"}[pasted], func(t *testing.T) {
				database, _, session := newManualHistoryTestSession(t)
				if pasted {
					session.recordManualInput(script)
				} else {
					for _, line := range strings.SplitAfter(script, "\n") {
						session.recordManualInput(line)
					}
				}
				var leaked int
				if err := database.QueryRow(`SELECT COUNT(*) FROM command_requests WHERE command LIKE '%private-body%'`).Scan(&leaked); err != nil {
					t.Fatal(err)
				}
				if leaked != 0 {
					t.Fatal("heredoc body entered persisted command history")
				}
				if row := readManualHistoryRow(t, database); !strings.HasSuffix(row.command, "pwd") {
					t.Fatalf("history did not resume after delimiter: %#v", row)
				}
			})
		}
	}
}
