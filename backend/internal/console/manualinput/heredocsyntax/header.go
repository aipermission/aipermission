// Package heredocsyntax recognizes bounded shell heredoc headers without executing them.
package heredocsyntax

import (
	"errors"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

type Document struct {
	Delimiter string
	StripTabs bool
}

func Parse(command string, recoveryBudget int) []Document {
	if !strings.Contains(command, "<<") {
		return nil
	}
	// Parse returns a partial tree for an unfinished heredoc. Only parsed
	// redirections count; quoted text, arithmetic and here-strings never do.
	file, literals := parseHeredocHeader(command, recoveryBudget)
	var documents []Document
	syntax.Walk(file, func(node syntax.Node) bool {
		redirect, ok := node.(*syntax.Redirect)
		if !ok || (redirect.Op != syntax.Hdoc && redirect.Op != syntax.DashHdoc) || redirect.Word == nil {
			return true
		}
		if len(redirect.Word.Parts) == 0 || redirect.Word.Pos().IsRecovered() {
			return true
		}
		delimiter := unquoteDelimiter(redirect.Word.Parts, false, literals)
		documents = append(documents, Document{Delimiter: delimiter, StripTabs: redirect.Op == syntax.DashHdoc})
		return false
	})
	return documents
}

func parseHeredocHeader(source string, recoveryBudget int) (*syntax.File, map[string]string) {
	original := source
	caseContext := false
	literals := make(map[string]string)
	for {
		file, err := syntax.NewParser(syntax.RecoverErrors(recoveryBudget)).Parse(strings.NewReader(source), "")
		var parseError syntax.ParseError
		if !errors.As(err, &parseError) {
			return file, literals
		}
		if !caseContext && (parseError.Text == "a command can only contain words and redirects; encountered `)`" ||
			parseError.Text == "statements must be separated by &, ; or a newline") {
			// Supply the missing context for a case arm on a later line.
			source = "case _ in\n" + source
			caseContext = true
			continue
		}
		offset := int(parseError.Pos.Offset())
		if offset >= len(source) {
			return file, literals
		}
		if token, _, closing := strings.Cut(strings.TrimPrefix(parseError.Text, "`"), "` can only be used "); closing && strings.HasPrefix(source[offset:], token) {
			// Neutralize only a parser-identified out-of-context token.
			source = source[:offset] + ":" + source[offset+len(token):]
			continue
		}
		if parseError.Text != "expansions not allowed in heredoc words" {
			return file, literals
		}
		// Mask the complete literal expansion, without evaluating it.
		literal := literalExpansion(source[offset:])
		if literal == "" {
			return file, literals
		}
		marker := literalMarker(original, literals)
		literals[marker] = literal
		source = source[:offset] + marker + source[offset+len(literal):]
	}
}

func literalMarker(source string, literals map[string]string) string {
	for i := len(literals); ; i++ {
		marker := "__AIP_HEREDOC_" + strconv.Itoa(i) + "__"
		if _, used := literals[marker]; !used && !strings.Contains(source, marker) {
			return marker
		}
	}
}

func literalExpansion(source string) string {
	if part := literalPart(source); part != nil {
		return source[:part.End().Offset()]
	}
	// A trailing comment or another command may contain unfinished syntax.
	// Stop once the parser proves the first literal's complete boundary.
	for end := 2; end < len(source); end++ {
		if part := literalPart(source[:end]); part != nil && part.End().Offset() < uint(end) {
			return source[:part.End().Offset()]
		}
	}
	return ""
}

func literalPart(source string) syntax.WordPart {
	word, parseErr := syntax.NewParser().Document(strings.NewReader(source))
	if strings.HasPrefix(source, "<(") || strings.HasPrefix(source, ">(") {
		// Process substitutions are ordinary word syntax, not document syntax.
		for parsed, err := range syntax.NewParser().WordsSeq(strings.NewReader(source)) {
			if err != nil {
				return nil
			}
			word = parsed
			parseErr = nil
			break
		}
	}
	if parseErr != nil || word == nil || len(word.Parts) == 0 {
		return nil
	}
	part := word.Parts[0]
	switch part.(type) {
	case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst:
		end := int(part.End().Offset())
		if part.Pos().Offset() == 0 && end <= len(source) {
			return part
		}
	}
	return nil
}

func unquoteDelimiter(parts []syntax.WordPart, quoted bool, literals map[string]string) string {
	var result strings.Builder
	for _, part := range parts {
		switch value := part.(type) {
		case *syntax.Lit:
			result.WriteString(unquoteLiteral(value.Value, quoted, literals))
		case *syntax.SglQuoted:
			if value.Dollar {
				// A single quoted node cannot execute substitutions.
				decoded, _ := expand.Literal(&expand.Config{}, &syntax.Word{Parts: []syntax.WordPart{value}})
				result.WriteString(decoded)
			} else {
				result.WriteString(value.Value)
			}
		case *syntax.DblQuoted:
			result.WriteString(unquoteDelimiter(value.Parts, true, literals))
		}
	}
	return result.String()
}

func unquoteLiteral(value string, quoted bool, literals map[string]string) string {
	var result strings.Builder
next:
	for len(value) > 0 {
		// Restore only markers present in an original AST literal segment,
		// before quote removal can join other text into a colliding marker.
		for marker, literal := range literals {
			if strings.HasPrefix(value, marker) {
				result.WriteString(literal)
				value = value[len(marker):]
				continue next
			}
		}
		if value[0] == '\\' && len(value) > 1 && (!quoted || strings.ContainsRune("\\$`\"", rune(value[1]))) {
			value = value[1:]
		}
		result.WriteByte(value[0])
		value = value[1:]
	}
	return result.String()
}
