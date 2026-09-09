package observability

import (
	"context"
	"database/sql"
	"fmt"
)

type Appender func(*sql.Tx, string, *int64, int64, string, any) error

type ProjectionFailureHandler func(action string, err error)

type Coordinator struct {
	database            *sql.DB
	dispatcher          *Dispatcher
	redact              func(string) string
	onProjectionFailure ProjectionFailureHandler
}

func NewCoordinator(database *sql.DB, dispatcher *Dispatcher, redact func(string) string, onProjectionFailure ProjectionFailureHandler) *Coordinator {
	return &Coordinator{
		database:            database,
		dispatcher:          dispatcher,
		redact:              redact,
		onProjectionFailure: onProjectionFailure,
	}
}

func (c *Coordinator) WriteRequired(ctx context.Context, actorType string, tokenID *int64, runtimeID int64, action string, payload any) error {
	if c == nil || c.database == nil {
		return fmt.Errorf("audit database is unavailable")
	}
	event, err := BuildEvent(ctx, c.database, BuildInput{
		ActorType: actorType,
		TokenID:   tokenID,
		RuntimeID: runtimeID,
		Action:    action,
		Payload:   payload,
		Redact:    c.redact,
	})
	if err != nil {
		return err
	}
	if _, err := (Store{}).Append(ctx, c.database, event); err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	if c.dispatcher == nil {
		return nil
	}
	if _, err := c.dispatcher.DispatchOnce(ctx); err != nil {
		c.reportProjectionFailure(action, err)
		c.dispatcher.Notify()
	}
	return nil
}

func (c *Coordinator) WithTransaction(ctx context.Context, mutate func(*sql.Tx, Appender) error) error {
	if c == nil || c.database == nil {
		return fmt.Errorf("audit database is unavailable")
	}
	if mutate == nil {
		return fmt.Errorf("audited mutation is unavailable")
	}
	// The redactor is captured before BeginTx by the composition layer so
	// SQLCipher's single connection is never held while loading policy state.
	appendAudit := c.appender(ctx)
	tx, err := c.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin audited mutation: %w", err)
	}
	defer tx.Rollback()
	if err := mutate(tx, appendAudit); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit audited mutation: %w", err)
	}
	c.Project(ctx)
	return nil
}

func (c *Coordinator) WithMutation(
	ctx context.Context,
	actorType string,
	tokenID *int64,
	runtimeID int64,
	action string,
	payload func() any,
	mutate func(*sql.Tx) error,
) error {
	if c == nil || c.database == nil {
		return fmt.Errorf("audit database is unavailable")
	}
	if payload == nil || mutate == nil {
		return fmt.Errorf("audited mutation callbacks are unavailable")
	}
	return c.WithTransaction(ctx, func(tx *sql.Tx, appendAudit Appender) error {
		if err := mutate(tx); err != nil {
			return err
		}
		return appendAudit(tx, actorType, tokenID, runtimeID, action, payload())
	})
}

func (c *Coordinator) Project(ctx context.Context) {
	if c == nil || c.database == nil || c.dispatcher == nil {
		return
	}
	if _, err := c.dispatcher.DispatchOnce(ctx); err != nil {
		c.reportProjectionFailure("", err)
	}
	c.dispatcher.Notify()
}

func (c *Coordinator) appender(ctx context.Context) Appender {
	return func(tx *sql.Tx, actorType string, tokenID *int64, runtimeID int64, action string, payload any) error {
		event, err := BuildEvent(ctx, tx, BuildInput{
			ActorType: actorType,
			TokenID:   tokenID,
			RuntimeID: runtimeID,
			Action:    action,
			Payload:   payload,
			Redact:    c.redact,
		})
		if err != nil {
			return err
		}
		_, err = (Store{}).Append(ctx, tx, event)
		return err
	}
}

func (c *Coordinator) reportProjectionFailure(action string, err error) {
	if c.onProjectionFailure != nil {
		c.onProjectionFailure(action, err)
	}
}
