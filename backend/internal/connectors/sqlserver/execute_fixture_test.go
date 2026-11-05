package sqlserverconnector

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/golang-sql/sqlexp"
)

type executionFixture struct {
	plans                                       [][]executionResultFixture
	queries, setups                             []string
	connects, closes, prepares, reads, finishes int
	connectErr, setupErr, prepareErr, queryErr  error
	closeErr, stmtCloseErr, finishErr           error
	transactionCount, transactionState          int64
	deadline                                    time.Time
	messages                                    *sqlexp.ReturnMessage
	messageCheckErr                             error
	block                                       bool
}

type executionResultFixture struct {
	columns, kinds []string
	values         [][]driver.Value
	count          *int64
	notices        int
	rowErr         error
	message        sqlexp.RawMessage
}

func (f *executionFixture) Connect(ctx context.Context) (driver.Conn, error) {
	f.connects++
	f.deadline, _ = ctx.Deadline()
	return f, f.connectErr
}
func (*executionFixture) Driver() driver.Driver { return nil }
func (*executionFixture) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("legacy prepare must not be used")
}
func (*executionFixture) Begin() (driver.Tx, error) {
	return nil, errors.New("connector must not introduce a transaction")
}
func (f *executionFixture) Close() error { f.closes++; return f.closeErr }
func (f *executionFixture) PrepareContext(_ context.Context, query string) (driver.Stmt, error) {
	f.prepares++
	return &executionStatementFixture{fixture: f, query: query}, f.prepareErr
}
func (f *executionFixture) CheckNamedValue(value *driver.NamedValue) error {
	if f.messageCheckErr != nil {
		return f.messageCheckErr
	}
	f.messages = value.Value.(*sqlexp.ReturnMessage)
	sqlexp.ReturnMessageInit(f.messages)
	return driver.ErrRemoveArgument
}

type executionStatementFixture struct {
	fixture *executionFixture
	query   string
}

func (s *executionStatementFixture) Close() error { return s.fixture.stmtCloseErr }
func (*executionStatementFixture) NumInput() int  { return -1 }
func (*executionStatementFixture) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("legacy execution must not be used")
}
func (*executionStatementFixture) Query([]driver.Value) (driver.Rows, error) {
	return nil, errors.New("legacy query must not be used")
}
func (s *executionStatementFixture) ExecContext(_ context.Context, args []driver.NamedValue) (driver.Result, error) {
	if len(args) != 0 {
		return nil, errors.New("unexpected init arguments")
	}
	s.fixture.setups = append(s.fixture.setups, s.query)
	return driver.RowsAffected(0), s.fixture.setupErr
}
func (s *executionStatementFixture) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	f := s.fixture
	if len(args) != 0 {
		return nil, errors.New("message argument was sent as SQL parameter")
	}
	if s.query == executionTransactionSQL {
		f.finishes++
		return &resultFixture{columns: []string{"count", "state"}, data: [][]driver.Value{{f.transactionCount, f.transactionState}}, queryErr: f.finishErr}, f.finishErr
	}
	f.queries = append(f.queries, s.query)
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	index := len(f.queries) - 1
	if index >= len(f.plans) {
		return nil, fmt.Errorf("unexpected batch %d", index)
	}
	rows := &executionRowsFixture{fixture: f, ctx: ctx, messages: f.messages, results: f.plans[index]}
	return rows, rows.announce()
}

type executionRowsFixture struct {
	fixture       *executionFixture
	ctx           context.Context
	messages      *sqlexp.ReturnMessage
	results       []executionResultFixture
	result, row   int
	announcedDone bool
}

func (r *executionRowsFixture) enqueue(message sqlexp.RawMessage) error {
	return sqlexp.ReturnMessageEnqueue(r.ctx, r.messages, message)
}
func (r *executionRowsFixture) announce() error {
	if r.result >= len(r.results) {
		return r.enqueue(sqlexp.MsgNextResultSet{})
	}
	result := r.results[r.result]
	if result.message != nil {
		if err := r.enqueue(result.message); err != nil {
			return err
		}
		return r.end()
	}
	if len(result.columns) != 0 {
		return r.enqueue(sqlexp.MsgNext{})
	}
	return r.end()
}
func (r *executionRowsFixture) end() error {
	if r.announcedDone {
		return nil
	}
	r.announcedDone = true
	if count := r.results[r.result].count; count != nil {
		if err := r.enqueue(sqlexp.MsgRowsAffected{Count: *count}); err != nil {
			return err
		}
	}
	return r.enqueue(sqlexp.MsgNextResultSet{})
}
func (r *executionRowsFixture) Columns() []string { return r.results[r.result].columns }
func (r *executionRowsFixture) ColumnTypeDatabaseTypeName(i int) string {
	return r.results[r.result].kinds[i]
}
func (*executionRowsFixture) Close() error { return nil }
func (r *executionRowsFixture) Next(values []driver.Value) error {
	result := r.results[r.result]
	if result.rowErr != nil {
		return result.rowErr
	}
	if r.row == 0 {
		for i := 0; i < result.notices; i++ {
			if err := r.enqueue(sqlexp.MsgNotice{Message: fixtureNotice(fmt.Sprintf("PRINT %d", i))}); err != nil {
				return err
			}
		}
	}
	if r.row >= len(result.values) {
		if err := r.end(); err != nil {
			return err
		}
		return io.EOF
	}
	copy(values, result.values[r.row])
	r.row++
	r.fixture.reads++
	return nil
}
func (*executionRowsFixture) HasNextResultSet() bool { return true }
func (r *executionRowsFixture) NextResultSet() error {
	r.result++
	r.row, r.announcedDone = 0, false
	if r.result >= len(r.results) {
		return io.EOF
	}
	return r.announce()
}

type fixtureNotice string

func (s fixtureNotice) String() string { return string(s) }

func fixtureCount(count int64) *int64 { return &count }
