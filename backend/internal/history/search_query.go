package history

import (
	"strings"
	"unicode"
)

const maxTerms = 8

func FTS(query string) string {
	terms := Terms(query, maxTerms)
	if len(terms) == 0 {
		return ""
	}
	return strings.Join(terms, " ")
}

func Terms(query string, limit int) []string {
	var terms []string
	var builder strings.Builder
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		terms = append(terms, strings.ToLower(builder.String()))
		builder.Reset()
	}
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			builder.WriteRune(r)
			continue
		}
		flush()
		if len(terms) >= limit {
			return terms
		}
	}
	flush()
	if len(terms) > limit {
		return terms[:limit]
	}
	return terms
}
