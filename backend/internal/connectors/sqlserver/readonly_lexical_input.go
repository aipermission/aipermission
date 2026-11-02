package sqlserverconnector

import (
	"fmt"
	"strings"
)

// Bound recursive comments before ANTLR lexing, and adapt escaped ANSI quoted
// names to equivalent brackets. The pinned grammar does not tokenize doubled
// double quotes atomically. This adapter never decides whether SQL is read-only.
func prepareSQLLexicalInput(query string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(query); {
		switch {
		case query[i] == '\'' || query[i] == '[' || query[i] == '"':
			end, err := sqlQuotedEnd(query, i)
			if err != nil {
				return "", err
			}
			span := query[i:end]
			if query[i] == '"' {
				name := strings.ReplaceAll(span[1:len(span)-1], `""`, `"`)
				out.WriteString("[" + strings.ReplaceAll(name, "]", "]]") + "]")
			} else {
				out.WriteString(span)
			}
			i = end
		case strings.HasPrefix(query[i:], "--"):
			out.WriteByte(' ')
			for i < len(query) && query[i] != '\n' && query[i] != '\r' {
				i++
			}
		case strings.HasPrefix(query[i:], "/*"):
			end, err := sqlCommentEnd(query, i)
			if err != nil {
				return "", err
			}
			out.WriteByte(' ')
			i = end
		default:
			out.WriteByte(query[i])
			i++
		}
	}
	return out.String(), nil
}

func sqlQuotedEnd(query string, start int) (int, error) {
	closing := query[start]
	if closing == '[' {
		closing = ']'
	}
	for i := start + 1; i < len(query); i++ {
		if query[i] != closing {
			continue
		}
		if i+1 < len(query) && query[i+1] == closing {
			i++
			continue
		}
		return i + 1, nil
	}
	return 0, fmt.Errorf("query_readonly requires closed string and identifier delimiters")
}

func sqlCommentEnd(query string, start int) (int, error) {
	depth := 1
	for i := start + 2; i < len(query)-1; i++ {
		switch query[i : i+2] {
		case "/*":
			depth++
			i++
		case "*/":
			depth--
			i++
			if depth == 0 {
				return i + 1, nil
			}
		}
		if depth > 16 {
			return 0, fmt.Errorf("query_readonly comment nesting exceeds 16 levels")
		}
	}
	return 0, fmt.Errorf("query_readonly requires closed block comments")
}
