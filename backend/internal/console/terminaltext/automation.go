package terminaltext

import (
	"fmt"
	"strings"
)

const ExecPrelude = "__aipermission_saved_stty=$(stty -g 2>/dev/null || true)\nstty -echo 2>/dev/null || true\n"

func ExecPayload(command, marker string) string {
	delimiter := marker + "_SCRIPT"
	for strings.Contains(command, "\n"+delimiter+"\n") {
		delimiter += "_X"
	}
	return fmt.Sprintf("/bin/sh <<'%s'\n(\n%s\n) </dev/null\n__aipermission_exit=$?\nprintf '\\n%s:%%s\\n' \"$__aipermission_exit\"\nunset __aipermission_exit\n%s\nif [ -n \"$__aipermission_saved_stty\" ]; then stty \"$__aipermission_saved_stty\" 2>/dev/null || true; else stty sane 2>/dev/null || stty echo icanon opost 2>/dev/null || true; fi; unset __aipermission_saved_stty\n", delimiter, command, marker, delimiter)
}
