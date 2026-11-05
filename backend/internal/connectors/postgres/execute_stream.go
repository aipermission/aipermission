package postgresconnector

import (
	"context"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// PgConn's generic MultiResultReader silently discards COPY output and waits
// for input on COPY IN. Use its public message/codec APIs to refuse interactive
// transfer immediately, while retaining native notice, error and TxStatus handling.
func (s *postgresExecuteSession) receiveExecution(ctx context.Context, collector *sqlexecute.Collector) error {
	var fields []pgconn.FieldDescription
	var failure error
	for {
		message, err := s.conn.PgConn().ReceiveMessage(ctx)
		if err != nil {
			return errors.Join(failure, err)
		}
		switch message := message.(type) {
		case *pgproto3.RowDescription:
			if fields != nil {
				return fmt.Errorf("postgres returned overlapping result metadata")
			}
			columns := make([]string, len(message.Fields))
			fields = make([]pgconn.FieldDescription, len(message.Fields))
			for index, field := range message.Fields {
				columns[index] = string(field.Name)
				fields[index] = pgconn.FieldDescription{Name: columns[index], DataTypeOID: field.DataTypeOID, Format: field.Format}
			}
			if err := collector.BeginResult(columns); err != nil {
				return err
			}
			for _, column := range columns {
				if !s.supportsResultText(column) {
					s.omitEncodedResult(collector)
					break
				}
			}
		case *pgproto3.DataRow:
			if fields == nil || len(fields) != len(message.Values) {
				return fmt.Errorf("postgres row does not match result metadata")
			}
			if collector.RetainingRows() {
				if err := s.collectExecutionRow(fields, message.Values, collector); err != nil {
					return err
				}
			}
		case *pgproto3.CommandComplete:
			if fields != nil {
				collector.EndResult()
				fields = nil
			}
			tag := pgconn.NewCommandTag(string(message.CommandTag))
			collector.Command(tag.String(), tag.RowsAffected())
		case *pgproto3.ErrorResponse:
			if failure == nil {
				failure = pgconn.ErrorResponseToPgError(message)
			}
			if fields != nil {
				collector.EndResult()
				fields = nil
			}
		case *pgproto3.ReadyForQuery:
			if fields != nil {
				return fmt.Errorf("postgres result ended without a command acknowledgement")
			}
			return failure
		case *pgproto3.CopyInResponse, *pgproto3.CopyOutResponse, *pgproto3.CopyBothResponse:
			// Close the socket first: the peer may be blocked sending COPY data,
			// so attempting a graceful Terminate write could wait for the deadline.
			_ = s.conn.PgConn().Conn().Close()
			return errors.Join(fmt.Errorf("COPY client transfer is not supported by execute_sql; connection closed, reconcile before retrying"), s.conn.PgConn().Close(ctx))
		case *pgproto3.ParameterStatus:
			if message.Name == "client_encoding" && fields != nil && !s.supportsResultText("") {
				s.omitEncodedResult(collector)
			}
		case *pgproto3.NoticeResponse, *pgproto3.NotificationResponse, *pgproto3.EmptyQueryResponse:
			// PgConn already delivered callbacks and updated connection state.
		default:
			return fmt.Errorf("unsupported postgres SQL response %T", message)
		}
	}
}

func (s *postgresExecuteSession) collectExecutionRow(fields []pgconn.FieldDescription, raw [][]byte, collector *sqlexecute.Collector) error {
	values := make([]any, len(raw))
	fallback := false
	for index, bytes := range raw {
		field := fields[index]
		if !s.supportsResultText(string(bytes)) || field.Format != 0 {
			s.omitEncodedResult(collector)
			return nil
		}
		if err := s.conn.TypeMap().Scan(field.DataTypeOID, field.Format, bytes, &values[index]); err != nil {
			// Output GUCs can change text representations. Keep the server's text
			// instead of interrupting an already-dispatched mutation script.
			values[index], fallback = string(bytes), true
		} else {
			values[index] = normalizePostgresValue(values[index])
		}
	}
	if fallback {
		collector.Notice("Some PostgreSQL values are retained as server text because their output format is not supported by the native decoder.")
	}
	return collector.AddRow(values)
}
