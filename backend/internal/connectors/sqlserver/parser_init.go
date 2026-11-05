package sqlserverconnector

import tsql "github.com/bytebase/parser/tsql"

// Generated immutable grammar initialization is sync.Once-owned by upstream.
// Pay its fixed cost at connector construction, not from a user's parse budget.
func initializeSQLGrammar() {
	tsql.TSqlLexerInit()
	tsql.TSqlParserInit()
}
