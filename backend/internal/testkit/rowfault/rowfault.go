package rowfault

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
)

var ErrIteration = errors.New("injected SQL row iteration failure")

type Result struct {
	Columns        []string
	Rows           [][]driver.Value
	IterationError error
}

type Query func(string) Result

func Open(query Query) *sql.DB {
	return sql.OpenDB(connector{query: query})
}

type connector struct{ query Query }

func (c connector) Connect(context.Context) (driver.Conn, error) {
	return connection{query: c.query}, nil
}

func (c connector) Driver() driver.Driver { return c }

func (c connector) Open(string) (driver.Conn, error) { return connection{query: c.query}, nil }

type connection struct{ query Query }

func (c connection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement in row fault test")
}

func (c connection) Close() error { return nil }

func (c connection) Begin() (driver.Tx, error) { return transaction{}, nil }

func (c connection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return transaction{}, nil
}

func (c connection) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	result := c.query(query)
	return &rows{result: result}, nil
}

func (connection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("unexpected mutation after row iteration failure")
}

type transaction struct{}

func (transaction) Commit() error   { return nil }
func (transaction) Rollback() error { return nil }

type rows struct {
	result Result
	index  int
}

func (r *rows) Columns() []string { return r.result.Columns }
func (r *rows) Close() error      { return nil }

func (r *rows) Next(dest []driver.Value) error {
	if r.index < len(r.result.Rows) {
		copy(dest, r.result.Rows[r.index])
		r.index++
		return nil
	}
	if r.result.IterationError != nil {
		return r.result.IterationError
	}
	return io.EOF
}
