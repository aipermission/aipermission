package postgresconnector

import (
	"strconv"
	"strings"
)

// Literal quoting within PL/pgSQL does not protect its outer dollar delimiter.
func provisionDOBlock(body string) string {
	tag := "$aipermission$"
	for index := 1; strings.Contains(body, tag); index++ {
		tag = "$aipermission_" + strconv.Itoa(index) + "$"
	}
	return "DO " + tag + body + tag
}
