// Package sqlexecute owns the shared explicit SQL mutation contract, not dialects.
package sqlexecute

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Action         = "execute_sql"
	MaxSQLBytes    = 64000
	MaxRows        = 1000
	MaxBatches     = 32
	MaxOutputBytes = 500000
	MaxCellBytes   = 64000
)

func Definition(label string) connectors.ActionDefinition {
	return connectors.ActionDefinition{
		Name: Action, Label: "Execute SQL", Category: "query", Risk: connectors.RiskWrite,
		Description: "Execute " + label + " SQL, including writes, schema changes and routines. May be destructive or partially committed; not a SQL sandbox.",
		InputSchema: connectors.Schema{Fields: []connectors.Field{
			{Name: "sql", Label: "SQL script", Type: connectors.FieldMultiline, Required: true, PreserveWhitespace: true},
			{Name: "max_rows", Label: "Maximum returned rows", Type: connectors.FieldInteger, Default: 100},
			{Name: "timeout_seconds", Label: "Execution timeout", Type: connectors.FieldInteger, Default: 30},
		}},
		OutputHint:    connectors.OutputHint{Format: "json", MaxRows: MaxRows, MaxBytes: MaxOutputBytes, FinalMaxBytes: MaxOutputBytes},
		RetryPolicy:   connectors.RetryPolicy{Class: connectors.RetryNonIdempotent},
		MaxInputBytes: 8*MaxSQLBytes + 4096,
	}
}

func Prepare(req connectors.ActionRequest, kind, label string) (connectors.PreparedAction, error) {
	if req.ActionName != Action {
		return connectors.PreparedAction{}, fmt.Errorf("unsupported SQL execution action")
	}
	payload, err := connectors.NormalizeSchemaValues(Definition(label).InputSchema, req.Input)
	if err != nil {
		return connectors.PreparedAction{}, err
	}
	sql, _ := payload["sql"].(string)
	if strings.TrimSpace(sql) == "" || len(sql) > MaxSQLBytes || !utf8.ValidString(sql) || strings.ContainsRune(sql, 0) {
		return connectors.PreparedAction{}, fmt.Errorf("execute_sql requires valid SQL up to %d bytes without null bytes", MaxSQLBytes)
	}
	if _, _, err := Limits(payload); err != nil {
		return connectors.PreparedAction{}, err
	}
	return connectors.PreparedAction{
		ConnectorKind: kind, TargetRef: req.Target.Ref, ProfileID: req.Profile.ID,
		ActionName: Action, Risk: connectors.RiskWrite, Title: "Execute " + label + " SQL",
		Summary: "May modify data, schema, permissions or external state. Script controls transactions; do not retry an uncertain outcome.",
		Preview: payload, Payload: payload,
		ContextMaterial: map[string]any{"target_ref": req.Target.Ref, "profile_id": req.Profile.ID, "action_name": Action},
	}, nil
}

func Limits(payload map[string]any) (rows, seconds int, err error) {
	rows, ok := connectors.NativeIntValue(payload["max_rows"])
	if !ok || rows < 1 || rows > MaxRows {
		return 0, 0, fmt.Errorf("max_rows must be between 1 and %d", MaxRows)
	}
	seconds, ok = connectors.NativeIntValue(payload["timeout_seconds"])
	if !ok || seconds < 1 || seconds > 300 {
		return 0, 0, fmt.Errorf("timeout_seconds must be between 1 and 300")
	}
	return rows, seconds, nil
}
