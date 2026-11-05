package sqlserverconnector

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
	mssql "github.com/microsoft/go-mssqldb"
)

func executeSQL(ctx context.Context, runtime connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	batches, err := compileExecutionSQL(text(action.Payload, "sql"))
	if err != nil {
		return connectors.ActionResult{}, err
	}
	_, seconds, err := sqlexecute.Limits(action.Payload)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	runtime.Capabilities = executionCapabilities{RuntimeCapabilityResolver: runtime.Capabilities, cancel: cancel}
	return sqlexecute.Run(ctx, action, batches, func(ctx context.Context) (sqlexecute.Session, error) {
		connector, err := driverConnector(ctx, runtime, time.Duration(seconds)*time.Second)
		if err != nil {
			return nil, err
		}
		// Connect marks a fresh session for reset. Leave its SQL empty so only
		// our explicit selected-timeout initialization consumes that reset flag.
		connector.SessionInitSQL = ""
		// Native Connect authenticates exactly once. No database/sql retry/reset
		// machinery may replay user SQL or reset transaction state between GO's.
		session, err := openExecutionSession(ctx, connector, seconds)
		if err != nil {
			return nil, errors.Join(err, context.Cause(ctx))
		}
		return session, nil
	})
}

func openExecutionSession(ctx context.Context, connector driver.Connector, seconds int) (*executionSession, error) {
	conn, err := connector.Connect(ctx)
	if err != nil {
		// The pinned Connector can also return a typed nil *Conn on dial failure.
		native, isNative := conn.(*mssql.Conn)
		if conn != nil && (!isNative || native != nil) {
			_ = conn.Close()
		}
		return nil, err
	}
	session := &executionSession{conn: conn}
	if _, ok := conn.(driver.ConnPrepareContext); !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("SQL Server native contextual preparation is unavailable")
	}
	if _, ok := conn.(driver.NamedValueChecker); !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("SQL Server native message support is unavailable")
	}
	// The execution connector clears SessionInitSQL before Connect. Apply the
	// selected settings once, without ROWCOUNT or an implicit transaction.
	initSQL := fmt.Sprintf("SET QUOTED_IDENTIFIER ON; SET TEXTSIZE 131072; SET LOCK_TIMEOUT %d;", seconds*1000)
	stmt, err := session.prepare(ctx, initSQL)
	if err == nil {
		if exec, ok := stmt.(driver.StmtExecContext); ok {
			_, err = exec.ExecContext(ctx, nil)
		} else {
			err = fmt.Errorf("SQL Server native contextual execution is unavailable")
		}
		err = closeExecutionStatement(stmt, err)
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return session, nil
}
