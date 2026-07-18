// Package sqlsafe provides conservative SQL parsing helpers shared by data
// connectors. Connector packages still own their dialect-specific policy.
package sqlsafe

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidateReadOnly rejects empty, oversized, multi-statement, write-like, or
// unsupported SQL while ignoring terms inside comments and quoted values.
func ValidateReadOnly(sql string, actionName string, maxBytes int, allowedPrefixes []string, allowedDescription string, disallowedTerms *regexp.Regexp) error {
	if strings.TrimSpace(sql) == "" {
		return fmt.Errorf("%s sql is required", actionName)
	}
	if maxBytes > 0 && len(sql) > maxBytes {
		return fmt.Errorf("%s sql exceeds %d bytes", actionName, maxBytes)
	}
	if strings.ContainsRune(sql, '\x00') {
		return fmt.Errorf("%s sql contains invalid null byte", actionName)
	}

	normalized := strings.TrimSpace(stripTrailingStatementTerminator(stripLeadingComments(sql)))
	if normalized == "" {
		return fmt.Errorf("%s sql is required", actionName)
	}
	checkSQL := validationSQL(normalized)
	if strings.Contains(checkSQL, ";") {
		return fmt.Errorf("%s only accepts a single statement", actionName)
	}
	if disallowedTerms != nil && disallowedTerms.MatchString(checkSQL) {
		return fmt.Errorf("%s only accepts read-only SQL", actionName)
	}
	if !hasAllowedPrefix(strings.TrimSpace(checkSQL), allowedPrefixes) {
		if strings.TrimSpace(allowedDescription) == "" {
			allowedDescription = strings.Join(upperStrings(allowedPrefixes), ", ")
		}
		return fmt.Errorf("%s only accepts %s SQL", actionName, allowedDescription)
	}
	return nil
}

func hasAllowedPrefix(sql string, prefixes []string) bool {
	for _, prefix := range prefixes {
		prefix = strings.ToLower(strings.TrimSpace(prefix))
		if prefix != "" && (sql == prefix || strings.HasPrefix(sql, prefix+" ") || strings.HasPrefix(sql, prefix+"\n") || strings.HasPrefix(sql, prefix+"\t")) {
			return true
		}
	}
	return false
}

func validationSQL(sql string) string {
	var out strings.Builder
	out.Grow(len(sql))
	for i := 0; i < len(sql); {
		switch {
		case strings.HasPrefix(sql[i:], "--"):
			for i < len(sql) && sql[i] != '\n' {
				out.WriteByte(' ')
				i++
			}
		case strings.HasPrefix(sql[i:], "/*"):
			out.WriteString("  ")
			i += 2
			for i < len(sql) && !strings.HasPrefix(sql[i:], "*/") {
				if sql[i] == '\n' {
					out.WriteByte('\n')
				} else {
					out.WriteByte(' ')
				}
				i++
			}
			if strings.HasPrefix(sql[i:], "*/") {
				out.WriteString("  ")
				i += 2
			}
		case sql[i] == '\'':
			i = maskQuoted(sql, i, '\'', &out)
		case sql[i] == '"':
			i = maskQuoted(sql, i, '"', &out)
		case sql[i] == '`':
			i = maskQuoted(sql, i, '`', &out)
		case sql[i] == '$':
			if end := dollarQuoteEnd(sql, i); end > i {
				for i < end {
					out.WriteByte(' ')
					i++
				}
			} else {
				out.WriteByte(byteLower(sql[i]))
				i++
			}
		default:
			out.WriteByte(byteLower(sql[i]))
			i++
		}
	}
	return out.String()
}

func maskQuoted(sql string, start int, quote byte, out *strings.Builder) int {
	i := start
	if i < len(sql) {
		out.WriteByte(' ')
		i++
	}
	for i < len(sql) {
		if sql[i] == '\n' {
			out.WriteByte('\n')
		} else {
			out.WriteByte(' ')
		}
		if sql[i] == quote {
			if i+1 < len(sql) && sql[i+1] == quote {
				i += 2
				out.WriteByte(' ')
				continue
			}
			i++
			break
		}
		i++
	}
	return i
}

func dollarQuoteEnd(sql string, start int) int {
	next := strings.IndexByte(sql[start+1:], '$')
	if next < 0 {
		return -1
	}
	tagEnd := start + 1 + next
	tag := sql[start : tagEnd+1]
	if !validDollarQuoteTag(tag) {
		return -1
	}
	closing := strings.Index(sql[tagEnd+1:], tag)
	if closing < 0 {
		return -1
	}
	return tagEnd + 1 + closing + len(tag)
}

func validDollarQuoteTag(tag string) bool {
	if len(tag) < 2 || tag[0] != '$' || tag[len(tag)-1] != '$' {
		return false
	}
	for _, ch := range tag[1 : len(tag)-1] {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return false
		}
	}
	return true
}

func stripLeadingComments(sql string) string {
	for {
		sql = strings.TrimSpace(sql)
		switch {
		case strings.HasPrefix(sql, "--"):
			lineEnd := strings.IndexByte(sql, '\n')
			if lineEnd < 0 {
				return ""
			}
			sql = sql[lineEnd+1:]
		case strings.HasPrefix(sql, "/*"):
			commentEnd := strings.Index(sql, "*/")
			if commentEnd < 0 {
				return sql
			}
			sql = sql[commentEnd+2:]
		default:
			return sql
		}
	}
}

func stripTrailingStatementTerminator(sql string) string {
	sql = strings.TrimSpace(sql)
	if strings.HasSuffix(sql, ";") {
		return strings.TrimSpace(strings.TrimSuffix(sql, ";"))
	}
	return sql
}

func upperStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, strings.ToUpper(value))
		}
	}
	return result
}

func byteLower(ch byte) byte {
	if ch >= 'A' && ch <= 'Z' {
		return ch + ('a' - 'A')
	}
	return ch
}
