package postgresconnector

import (
	"strings"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
)

func (s *postgresExecuteSession) supportsResultText(text string) bool {
	encoding := s.conn.PgConn().ParameterStatus("client_encoding")
	return (encoding == "" || strings.EqualFold(encoding, "UTF8") || strings.EqualFold(encoding, "UTF-8")) && utf8.ValidString(text)
}

func (s *postgresExecuteSession) omitEncodedResult(collector *sqlexecute.Collector) {
	collector.OmitCurrentResult()
	collector.Notice("Result omitted: PostgreSQL returned unsupported client encoding or invalid UTF-8 text; remaining commands are still consumed.")
}
