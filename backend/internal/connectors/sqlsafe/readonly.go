// Package sqlsafe provides conservative SQL parsing helpers shared by data
// connectors. Connector packages still own their dialect-specific policy.
package sqlsafe

import (
	"fmt"
	"regexp"
	"strings"
)

type Dialect uint8

type FunctionCall struct {
	Schema string
	Name   string
}

const (
	DialectANSI Dialect = iota
	DialectPostgreSQL
)

// ValidateReadOnly rejects empty, oversized, multi-statement, write-like, or
// unsupported SQL while ignoring terms inside comments and quoted values.
func ValidateReadOnly(sql string, actionName string, maxBytes int, allowedPrefixes []string, allowedDescription string, disallowedTerms *regexp.Regexp) error {
	return ValidateReadOnlyDialect(sql, actionName, maxBytes, allowedPrefixes, allowedDescription, disallowedTerms, DialectANSI)
}

func ValidateReadOnlyDialect(sql string, actionName string, maxBytes int, allowedPrefixes []string, allowedDescription string, disallowedTerms *regexp.Regexp, dialect Dialect) error {
	if strings.TrimSpace(sql) == "" {
		return fmt.Errorf("%s sql is required", actionName)
	}
	if maxBytes > 0 && len(sql) > maxBytes {
		return fmt.Errorf("%s sql exceeds %d bytes", actionName, maxBytes)
	}
	if strings.ContainsRune(sql, '\x00') {
		return fmt.Errorf("%s sql contains invalid null byte", actionName)
	}

	normalized := strings.TrimSpace(stripTrailingStatementTerminator(sql))
	if normalized == "" {
		return fmt.Errorf("%s sql is required", actionName)
	}
	checkSQL, err := validationSQL(normalized, dialect)
	if err != nil {
		return fmt.Errorf("%s sql is malformed: %w", actionName, err)
	}
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

func validationSQL(sql string, dialect Dialect) (string, error) {
	return normalizedSQL(sql, dialect, false, false)
}

// PostgreSQLFunctionCalls returns function-shaped identifiers outside comments
// and quoted values. Quoted function identifiers are represented by a sentinel
// so callers can reject them without confusing their contents with SQL terms.
func PostgreSQLFunctionCalls(sql string) ([]FunctionCall, error) {
	normalized, err := normalizedSQL(sql, DialectPostgreSQL, true, false)
	if err != nil {
		return nil, err
	}
	calls := make([]FunctionCall, 0)
	for offset := 0; offset < len(normalized); {
		if !postgresIdentifierStart(normalized[offset]) || (offset > 0 && postgresIdentifierContinue(normalized[offset-1])) {
			offset++
			continue
		}
		first, end := postgresIdentifierAt(normalized, offset)
		cursor := skipSQLSpace(normalized, end)
		schema, name := "", first
		if cursor < len(normalized) && normalized[cursor] == '.' {
			cursor = skipSQLSpace(normalized, cursor+1)
			if cursor >= len(normalized) || !postgresIdentifierStart(normalized[cursor]) {
				offset = end
				continue
			}
			schema = first
			name, end = postgresIdentifierAt(normalized, cursor)
			cursor = skipSQLSpace(normalized, end)
		}
		if cursor >= len(normalized) || normalized[cursor] != '(' {
			offset = end
			continue
		}
		if schema == "" && isFunctionSyntaxKeyword(name) {
			offset = end
			continue
		}
		calls = append(calls, FunctionCall{Schema: schema, Name: name})
		// Continue at the opening parenthesis so nested calls are discovered.
		offset = cursor + 1
	}
	return calls, nil
}

func postgresIdentifierAt(sql string, start int) (string, int) {
	end := start + 1
	for end < len(sql) && postgresIdentifierContinue(sql[end]) {
		end++
	}
	return sql[start:end], end
}

func postgresIdentifierStart(ch byte) bool {
	return ch == '_' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= 0x80
}

func postgresIdentifierContinue(ch byte) bool {
	return postgresIdentifierStart(ch) || ch >= '0' && ch <= '9' || ch == '$'
}

func skipSQLSpace(sql string, start int) int {
	for start < len(sql) {
		switch sql[start] {
		case ' ', '\t', '\r', '\n', '\f':
			start++
		default:
			return start
		}
	}
	return start
}

// ValidatePostgreSQLResolutionSyntax rejects explicit operator and cast syntax
// whose implementation can resolve to user-defined code outside the visible
// function-call allowlist.
func ValidatePostgreSQLResolutionSyntax(sql string) error {
	normalized, err := normalizedSQL(sql, DialectPostgreSQL, true, true)
	if err != nil {
		return err
	}
	if postgresOperatorPattern.MatchString(normalized) {
		return fmt.Errorf("explicit operators are not allowed")
	}
	if strings.Contains(normalized, "::") || postgresCastPattern.MatchString(normalized) {
		return fmt.Errorf("explicit casts are not allowed")
	}
	if postgresQualifiedTypedLiteralPattern.MatchString(normalized) {
		return fmt.Errorf("schema-qualified typed literals are not allowed")
	}
	return nil
}

var postgresOperatorPattern = regexp.MustCompile(`(?i)(?:^|[^\pL\pN_$])operator\s*\(`)
var postgresCastPattern = regexp.MustCompile(`(?i)(?:^|[^\pL\pN_$])cast\s*\(`)
var postgresQualifiedTypedLiteralPattern = regexp.MustCompile(`(?i)(?:^|[^\pL\pN_$])(?:[a-z_][a-z0-9_$]*|quoted_identifier)\s*\.\s*(?:[a-z_][a-z0-9_$]*|quoted_identifier)\s+(?:e\s*)?string_literal(?:\s|$)`)

func isFunctionSyntaxKeyword(value string) bool {
	switch strings.ToLower(value) {
	case "as", "cast", "exists", "filter", "from", "group", "in", "over", "select", "values", "when", "where", "with", "within":
		return true
	default:
		return false
	}
}

func normalizedSQL(sql string, dialect Dialect, preserveQuotedIdentifiers bool, preserveStringMarkers bool) (string, error) {
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
			depth := 1
			for i < len(sql) && depth > 0 {
				if dialect == DialectPostgreSQL && strings.HasPrefix(sql[i:], "/*") {
					out.WriteString("  ")
					i += 2
					depth++
					continue
				}
				if strings.HasPrefix(sql[i:], "*/") {
					out.WriteString("  ")
					i += 2
					depth--
					continue
				}
				if sql[i] == '\n' {
					out.WriteByte('\n')
				} else {
					out.WriteByte(' ')
				}
				i++
			}
			if depth > 0 {
				return "", fmt.Errorf("unterminated block comment")
			}
		case sql[i] == '\'':
			var closed bool
			if preserveStringMarkers {
				var discarded strings.Builder
				i, closed = maskQuoted(sql, i, '\'', postgresEscapeStringAt(sql, i, dialect), &discarded)
				out.WriteString(" string_literal ")
			} else {
				i, closed = maskQuoted(sql, i, '\'', postgresEscapeStringAt(sql, i, dialect), &out)
			}
			if !closed {
				return "", fmt.Errorf("unterminated single-quoted value")
			}
		case sql[i] == '"':
			var closed bool
			if preserveQuotedIdentifiers {
				i, closed = maskQuotedIdentifier(sql, i, &out)
			} else {
				i, closed = maskQuoted(sql, i, '"', false, &out)
			}
			if !closed {
				return "", fmt.Errorf("unterminated quoted identifier")
			}
		case sql[i] == '`':
			var closed bool
			i, closed = maskQuoted(sql, i, '`', false, &out)
			if !closed {
				return "", fmt.Errorf("unterminated quoted identifier")
			}
		case sql[i] == '$':
			end, opener := dollarQuoteEnd(sql, i)
			if opener && end < 0 {
				return "", fmt.Errorf("unterminated dollar-quoted value")
			}
			if end > i {
				if preserveStringMarkers {
					out.WriteString(" string_literal ")
					i = end
				} else {
					for i < end {
						out.WriteByte(' ')
						i++
					}
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
	return out.String(), nil
}

func maskQuotedIdentifier(sql string, start int, out *strings.Builder) (int, bool) {
	out.WriteString("quoted_identifier")
	for i := start + 1; i < len(sql); i++ {
		if sql[i] != '"' {
			continue
		}
		if i+1 < len(sql) && sql[i+1] == '"' {
			i++
			continue
		}
		return i + 1, true
	}
	return len(sql), false
}

func maskQuoted(sql string, start int, quote byte, backslashEscapes bool, out *strings.Builder) (int, bool) {
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
		if backslashEscapes && sql[i] == '\\' {
			i++
			if i >= len(sql) {
				return i, false
			}
			if sql[i] == '\n' {
				out.WriteByte('\n')
			} else {
				out.WriteByte(' ')
			}
			i++
			continue
		}
		if sql[i] == quote {
			if i+1 < len(sql) && sql[i+1] == quote {
				i += 2
				out.WriteByte(' ')
				continue
			}
			i++
			return i, true
		}
		i++
	}
	return i, false
}

func postgresEscapeStringAt(sql string, quote int, dialect Dialect) bool {
	if dialect != DialectPostgreSQL || quote < 1 || (sql[quote-1] != 'e' && sql[quote-1] != 'E') {
		return false
	}
	return quote < 2 || !isIdentifierByte(sql[quote-2])
}

func isIdentifierByte(ch byte) bool {
	return ch == '_' || ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
}

func dollarQuoteEnd(sql string, start int) (int, bool) {
	next := strings.IndexByte(sql[start+1:], '$')
	if next < 0 {
		return -1, false
	}
	tagEnd := start + 1 + next
	tag := sql[start : tagEnd+1]
	if !validDollarQuoteTag(tag) {
		return -1, false
	}
	closing := strings.Index(sql[tagEnd+1:], tag)
	if closing < 0 {
		return -1, true
	}
	return tagEnd + 1 + closing + len(tag), true
}

func validDollarQuoteTag(tag string) bool {
	if len(tag) < 2 || tag[0] != '$' || tag[len(tag)-1] != '$' {
		return false
	}
	body := tag[1 : len(tag)-1]
	if body == "" {
		return true
	}
	if !postgresIdentifierStart(body[0]) {
		return false
	}
	for index := 1; index < len(body); index++ {
		if !postgresIdentifierContinue(body[index]) || body[index] == '$' {
			return false
		}
	}
	return true
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
