package sqlserverconnector

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/antlr4-go/antlr/v4"
	tsql "github.com/bytebase/parser/tsql"
)

const maxSQLBytes = 20000

type sqlSyntaxErrors struct {
	*antlr.DefaultErrorListener
	failed bool
}

func (e *sqlSyntaxErrors) SyntaxError(antlr.Recognizer, interface{}, int, int, string, antlr.RecognitionException) {
	e.failed = true
}

func validateReadonlySQL(query string) error { _, err := compileReadonlySQL(query); return err }

// A complete SQL file is parsed with error recovery disabled by rejection. Only
// reviewed read rules may occur. Dispatch uses its token projection, never an
// AST printer or an incompletely parsed suffix of the original text.
func compileReadonlySQL(query string) (compiled string, err error) {
	defer func() {
		if recover() != nil {
			compiled, err = "", fmt.Errorf("query_readonly could not parse supported T-SQL")
		}
	}()
	if len(query) > maxSQLBytes || strings.TrimSpace(query) == "" || !utf8.ValidString(query) || strings.ContainsRune(query, 0) {
		return "", fmt.Errorf("query_readonly requires valid T-SQL up to %d bytes", maxSQLBytes)
	}
	query, err = prepareSQLLexicalInput(query)
	if err != nil {
		return "", fmt.Errorf("query_readonly lexical input: %w", err)
	}
	errors := &sqlSyntaxErrors{DefaultErrorListener: antlr.NewDefaultErrorListener()}
	lexer := tsql.NewTSqlLexer(antlr.NewInputStream(query))
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(errors)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	stream.Fill()
	parts, err := boundedSQLTokens(stream.GetAllTokens())
	if err != nil || errors.failed {
		return "", fmt.Errorf("query_readonly contains invalid or excessive T-SQL tokens")
	}
	parser := tsql.NewTSqlParser(stream)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(errors)
	root := parser.Tsql_file()
	if errors.failed {
		return "", fmt.Errorf("query_readonly requires valid supported T-SQL")
	}
	policy := sqlReadPolicy{parser: parser, identifiers: map[int]string{}}
	if err := policy.visit(root, 0); err != nil {
		return "", err
	}
	if policy.statements != 1 {
		return "", fmt.Errorf("query_readonly requires one SELECT or WITH SELECT statement")
	}
	part := 0
	for _, item := range stream.GetAllTokens() {
		if item.GetTokenType() == antlr.TokenEOF || item.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		if quoted, ok := policy.identifiers[item.GetTokenIndex()]; ok {
			parts[part] = quoted
		}
		part++
	}
	compiled = strings.Join(parts, " ")
	if len(compiled) > maxSQLBytes {
		return "", fmt.Errorf("query_readonly projected SQL exceeds %d bytes", maxSQLBytes)
	}
	return compiled, nil
}

func boundedSQLTokens(tokens []antlr.Token) ([]string, error) {
	parts := []string{}
	depth := 0
	if len(tokens) > 4096 {
		return nil, fmt.Errorf("too many tokens")
	}
	for _, item := range tokens {
		if item.GetTokenType() == antlr.TokenEOF || item.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		text := item.GetText()
		switch item.GetTokenType() {
		case tsql.TSqlLexerLR_BRACKET:
			depth++
		case tsql.TSqlLexerRR_BRACKET:
			depth--
		case tsql.TSqlLexerLOCAL_ID:
			if !readServerVariables[strings.ToLower(text)] {
				return nil, fmt.Errorf("variables are unsupported")
			}
		case tsql.TSqlLexerTEMP_ID:
			return nil, fmt.Errorf("temporary objects are unsupported")
		}
		if depth < 0 || depth > 32 || len(parts) >= 2048 {
			return nil, fmt.Errorf("SQL complexity limit exceeded")
		}
		parts = append(parts, text)
	}
	if depth != 0 {
		return nil, fmt.Errorf("unmatched parentheses")
	}
	return parts, nil
}

type sqlReadPolicy struct {
	parser        *tsql.TSqlParser
	statements    int
	identifiers   map[int]string
	preserveNames int
}

func (p *sqlReadPolicy) visit(tree antlr.Tree, depth int) error {
	if depth > 256 {
		return fmt.Errorf("query_readonly syntax nesting limit exceeded")
	}
	if node, ok := tree.(antlr.ParserRuleContext); ok {
		name := p.parser.RuleNames[node.GetRuleIndex()]
		if !readSQLRules[name] {
			return fmt.Errorf("query_readonly does not support %s", name)
		}
		if name == "select_statement_standalone" {
			p.statements++
		}
		if name == "data_type" || name == "scalar_function_name" {
			p.preserveNames++
			defer func() { p.preserveNames-- }()
		}
		// Dedicated tokens such as VALUE_SQUARE_BRACKET are also quoted names.
		if name == "id_" && p.preserveNames == 0 && !strings.HasPrefix(node.GetStart().GetText(), "[") {
			text := node.GetStart().GetText()
			p.identifiers[node.GetStart().GetTokenIndex()] = "[" + strings.ReplaceAll(text, "]", "]]") + "]"
		}
		if err := p.validateNode(node, name); err != nil {
			return err
		}
	}
	for _, child := range tree.GetChildren() {
		if err := p.visit(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func (p *sqlReadPolicy) validateNode(node antlr.ParserRuleContext, name string) error {
	switch name {
	case "function_call":
		if !readFunctions[strings.ToLower(node.GetStart().GetText())] {
			return fmt.Errorf("query_readonly accepts only approved unqualified built-in functions")
		}
	case "scalar_function_name":
		if !readFunctions[strings.ToLower(node.GetText())] {
			return fmt.Errorf("query_readonly rejects qualified or unreviewed functions")
		}
	case "join_on":
		if node.(*tsql.Join_onContext).GetJoin_hint() != nil {
			return fmt.Errorf("query_readonly rejects join hints")
		}
	case "table_source_item":
		if node.(*tsql.Table_source_itemContext).Function_call() != nil {
			return fmt.Errorf("query_readonly rejects table functions")
		}
	case "select_list_elem":
		if node.(*tsql.Select_list_elemContext).LOCAL_ID() != nil {
			return fmt.Errorf("query_readonly rejects variable assignment")
		}
	case "data_type":
		if !readTypes[strings.ToLower(node.GetStart().GetText())] {
			return fmt.Errorf("query_readonly casts require built-in scalar types")
		}
	case "full_table_name":
		n := node.(*tsql.Full_table_nameContext)
		if len(n.AllDotID()) > 1 || n.DoubleDotID() != nil {
			return fmt.Errorf("query_readonly accepts only current-database table references")
		}
	case "table_name":
		if node.(*tsql.Table_nameContext).GetDatabase() != nil {
			return fmt.Errorf("query_readonly does not accept cross-database table references")
		}
	case "query_specification":
		if node.(*tsql.Query_specificationContext).INTO() != nil {
			return fmt.Errorf("query_readonly rejects SELECT INTO")
		}
	}
	return nil
}

var readSQLRules = wordSet(`tsql_file batch_without_go sql_clauses dml_clause select_statement_standalone select_statement
 with_expression common_table_expression column_name_list query_expression query_specification select_list select_list_elem expression_elem
 expression primitive_expression primitive_constant constant asterisk id_ keyword dotID full_column_name column_alias as_column_alias
 column_alias_list table_alias as_table_alias from_table_sources table_sources table_source table_source_item full_table_name table_name
 join_part join_on cross_join search_condition predicate comparison_operator bracket_expression subquery top_clause top_count top_percent
 select_order_by_clause order_by_clause order_by_expression group_by_clause group_by_item having_clause sql_union
 case_expression switch_section switch_search_condition_section unary_operator_expression null_notnull function_call scalar_function_name
 func_proc_name_server_database_schema func_proc_name_database_schema func_proc_name_schema built_in_functions aggregate_windowed_function
 analytic_windowed_function ranking_windowed_function all_distinct_expression over_clause row_or_range_clause window_frame_extent window_frame_bound
 window_frame_preceding window_frame_following expression_list_ data_type datepart datepart_abbreviations dateparts_9 dateparts_12 dateparts_15 derived_table`)
