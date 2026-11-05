package sqlserverconnector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
	"github.com/antlr4-go/antlr/v4"
	tsql "github.com/bytebase/parser/tsql"
)

// The parse shadow has exactly one rune per original rune. Only original byte
// slices are dispatched; neither the read-only projection nor an AST printer
// may rewrite an approved script.
func compileExecutionSQL(ctx context.Context, script string) (batches []string, err error) {
	defer func() {
		if errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("execute_sql T-SQL validation deadline exceeded; no SQL was sent; simplify the script or retry validation under lower load: %w", err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	initializeSQLGrammar()
	ctx, cancel := context.WithTimeout(ctx, executionParserTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			batches, err = nil, fmt.Errorf("execute_sql exceeded supported T-SQL syntax complexity")
			if cause := context.Cause(ctx); cause != nil {
				err = cause
			}
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(script) > sqlexecute.MaxSQLBytes || strings.TrimSpace(script) == "" || !utf8.ValidString(script) || strings.ContainsRune(script, 0) {
		return nil, fmt.Errorf("execute_sql requires valid T-SQL up to %d bytes", sqlexecute.MaxSQLBytes)
	}
	shadow, offsets, err := executionLexicalInput(script)
	if err != nil {
		return nil, err
	}
	errors := &sqlSyntaxErrors{DefaultErrorListener: antlr.NewDefaultErrorListener()}
	lexer := tsql.NewTSqlLexer(antlr.NewInputStream(string(shadow)))
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(errors)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	stream.Fill()
	if err := boundExecutionTokens(stream.GetAllTokens()); err != nil || errors.failed {
		return nil, fmt.Errorf("execute_sql contains invalid or excessive T-SQL tokens")
	}
	bounded := &executionTokenStream{CommonTokenStream: stream, ctx: ctx}
	root := parseExecutionRoot(bounded, errors, antlr.PredictionModeSLL)
	if errors.failed {
		// Some valid grammar alternatives need full context. Both attempts share
		// the same work/deadline budget; fallback never renews an exhausted budget.
		errors.failed = false
		bounded.Seek(0)
		root = parseExecutionRoot(bounded, errors, antlr.PredictionModeLL)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if errors.failed {
		return nil, fmt.Errorf("execute_sql requires a complete supported T-SQL script")
	}
	separators := executionSeparators(shadow, stream.GetAllTokens(), root.AllGo_statement())
	return splitExecutionBatches(script, shadow, offsets, separators)
}

func parseExecutionRoot(stream antlr.TokenStream, errors *sqlSyntaxErrors, mode int) tsql.ITsql_fileContext {
	parser := tsql.NewTSqlParser(stream)
	parser.GetInterpreter().SetPredictionMode(mode)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(errors)
	parser.AddParseListener(&executionDepth{BaseParseTreeListener: &antlr.BaseParseTreeListener{}})
	return parser.Tsql_file()
}

func executionLexicalInput(script string) ([]rune, []int, error) {
	shadow := []rune(script)
	offsets := make([]int, 0, len(shadow)+1)
	for i := range script {
		offsets = append(offsets, i)
	}
	offsets = append(offsets, len(script))
	lineStart := true
	for i, runeIndex := 0, 0; i < len(script); {
		end, comment, err := executionSpan(script, i)
		if err != nil {
			return nil, nil, fmt.Errorf("execute_sql lexical input: %w", err)
		}
		if end > i {
			span := []rune(script[i:end])
			for j, r := range span {
				if comment && r != '\r' && r != '\n' {
					shadow[runeIndex+j] = ' '
				} else if script[i] == '"' && j > 0 && j < len(span)-1 && r == '"' {
					shadow[runeIndex+j] = '_'
				}
				if r == '\n' || r == '\r' {
					lineStart = true
				} else if !comment {
					lineStart = false
				}
			}
			runeIndex += utf8.RuneCountInString(script[i:end])
			i = end
			continue
		}
		if strings.HasPrefix(script[i:], "$(") || lineStart && (script[i] == ':' || strings.HasPrefix(script[i:], "!!")) {
			return nil, nil, fmt.Errorf("execute_sql does not support SQLCMD directives or substitutions")
		}
		r, size := utf8.DecodeRuneInString(script[i:])
		if r == '\r' || r == '\n' {
			lineStart = true
		} else if r != ' ' && r != '\t' {
			lineStart = false
		}
		i, runeIndex = i+size, runeIndex+1
	}
	return shadow, offsets, nil
}

func executionSpan(script string, i int) (end int, comment bool, err error) {
	switch {
	case script[i] == '\'' || script[i] == '[' || script[i] == '"':
		end, err = sqlQuotedEnd(script, i)
	case strings.HasPrefix(script[i:], "/*"):
		end, err = sqlCommentEnd(script, i)
		comment = true
	case strings.HasPrefix(script[i:], "--"):
		end = i + 2
		for end < len(script) && script[end] != '\r' && script[end] != '\n' {
			end++
		}
		comment = true
	}
	return
}

func boundExecutionTokens(tokens []antlr.Token) error {
	visible, depth := 0, 0
	for _, token := range tokens {
		if token.GetChannel() != antlr.TokenDefaultChannel || token.GetTokenType() == antlr.TokenEOF {
			continue
		}
		visible++
		switch token.GetTokenType() {
		case tsql.TSqlLexerLR_BRACKET:
			depth++
		case tsql.TSqlLexerRR_BRACKET:
			depth--
		}
		if visible > 8192 || depth < 0 || depth > 32 {
			return fmt.Errorf("execute_sql token or nesting limit exceeded")
		}
	}
	if depth != 0 {
		return fmt.Errorf("execute_sql has unmatched parentheses")
	}
	return nil
}

type executionDepth struct {
	*antlr.BaseParseTreeListener
	depth int
}

func (d *executionDepth) EnterEveryRule(antlr.ParserRuleContext) {
	d.depth++
	if d.depth > 256 {
		panic("execute_sql parser depth exceeded")
	}
}

func (d *executionDepth) ExitEveryRule(antlr.ParserRuleContext) { d.depth-- }

func executionSeparators(shadow []rune, tokens []antlr.Token, parsed []tsql.IGo_statementContext) []antlr.Token {
	indices := map[int]bool{}
	for _, separator := range parsed {
		indices[separator.GetStart().GetTokenIndex()] = true
	}
	separators := []antlr.Token{}
	for _, token := range tokens {
		if token.GetTokenType() != tsql.TSqlLexerGO {
			continue
		}
		start := token.GetStart()
		for start > 0 && shadow[start-1] != '\r' && shadow[start-1] != '\n' {
			start--
		}
		// The grammar can also interpret a leading GO as a procedure name.
		// Client separators must be recognized before that ambiguity matters.
		if indices[token.GetTokenIndex()] || strings.TrimSpace(string(shadow[start:token.GetStart()])) == "" {
			separators = append(separators, token)
		}
	}
	return separators
}

func splitExecutionBatches(script string, shadow []rune, offsets []int, separators []antlr.Token) ([]string, error) {
	batches := []string{}
	start := 0
	for _, token := range separators {
		left, right := token.GetStart(), token.GetStop()+1
		lineStart, lineEnd := left, right
		for lineStart > 0 && shadow[lineStart-1] != '\n' && shadow[lineStart-1] != '\r' {
			lineStart--
		}
		for lineEnd < len(shadow) && shadow[lineEnd] != '\n' && shadow[lineEnd] != '\r' {
			lineEnd++
		}
		if strings.TrimSpace(string(shadow[lineStart:left])) != "" || strings.TrimSpace(string(shadow[right:lineEnd])) != "" {
			return nil, fmt.Errorf("execute_sql requires GO on its own line, without a repeat count")
		}
		if strings.TrimSpace(string(shadow[start:left])) == "" {
			return nil, fmt.Errorf("execute_sql does not support empty GO batches")
		}
		batches = append(batches, script[offsets[start]:offsets[left]])
		start = right
	}
	if strings.TrimSpace(string(shadow[start:])) != "" || len(batches) > 0 && strings.TrimSpace(script[offsets[start]:]) != "" {
		batches = append(batches, script[offsets[start]:])
	}
	if len(batches) == 0 || len(batches) > sqlexecute.MaxBatches {
		return nil, fmt.Errorf("execute_sql requires 1 to %d nonempty batches", sqlexecute.MaxBatches)
	}
	return batches, nil
}
