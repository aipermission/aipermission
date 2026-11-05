package sqlexecute

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlresult"
)

// Session is action-scoped. Dialects own dispatch and protocol observation;
// core permissions, approval, history and audit remain connector-independent.
type Session interface {
	Execute(context.Context, string, *Collector) error
	Finish(context.Context) error
	Close(context.Context) error
}

func Run(ctx context.Context, action connectors.PreparedAction, batches []string, open func(context.Context) (Session, error)) (connectors.ActionResult, error) {
	rows, seconds, err := Limits(action.Payload)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if action.ActionName != Action || action.Risk != connectors.RiskWrite || len(batches) < 1 || len(batches) > MaxBatches {
		return connectors.ActionResult{}, fmt.Errorf("invalid explicit SQL execution plan")
	}
	total := 0
	for _, batch := range batches {
		total += len(batch)
		if strings.TrimSpace(batch) == "" || len(batch) > MaxSQLBytes {
			return connectors.ActionResult{}, fmt.Errorf("invalid SQL batch")
		}
	}
	if total > MaxSQLBytes {
		return connectors.ActionResult{}, fmt.Errorf("SQL batches exceed the input limit")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return connectors.ActionResult{}, err
	}
	session, err := open(ctx)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stop()
		_ = session.Close(cleanup)
	}()
	collector := NewCollector(rows)
	completed := 0
	for _, batch := range batches {
		if err := ctx.Err(); err != nil {
			if completed == 0 {
				return connectors.ActionResult{}, err
			}
			return unknown(collector, completed, err), nil
		}
		if err := session.Execute(ctx, batch, collector); err != nil {
			return unknown(collector, completed, err), nil
		}
		completed++
	}
	if err := session.Finish(ctx); err != nil {
		return unknown(collector, completed, err), nil
	}
	output := collector.Output(completed)
	if !outputFits(output) {
		return unknown(NewCollector(rows), completed, fmt.Errorf("SQL response exceeded the serialized output limit")), nil
	}
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: output,
		DisplayText: fmt.Sprintf("SQL execution acknowledged; %d batch(es), %d returned row(s).", completed, collector.rows)}, nil
}

func unknown(collector *Collector, completed int, err error) connectors.ActionResult {
	collector.EndResult()
	output := collector.Output(completed)
	if !outputFits(output) {
		output = map[string]any{"completed_batches": completed, "truncated": true}
	}
	message, _ := sqlresult.BoundValue("SQL may have partially committed; reconcile before retrying: "+err.Error(), 2048, "...[truncated]")
	return connectors.OutcomeUnknownResult("sql_execution", output, fmt.Errorf("%s", message))
}

func outputFits(output map[string]any) bool {
	encoded, err := json.Marshal(output)
	return err == nil && len(encoded) <= MaxOutputBytes-32768
}
