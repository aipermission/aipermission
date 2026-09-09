package console

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/console/terminaltext"
)

func scanConsoleSession(scanner interface {
	Scan(dest ...any) error
}) (Record, error) {
	var item Record
	var closedAt sql.NullString
	if err := scanner.Scan(
		&item.ID,
		&item.RuntimeID,
		&item.Generation,
		&item.TargetName,
		&item.Name,
		&item.Status,
		&item.Transcript,
		&item.Error,
		&item.Cols,
		&item.Rows,
		&item.CreatedAt,
		&item.UpdatedAt,
		&closedAt,
		&item.EnvironmentContentHash,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Record{}, err
		}
		return Record{}, fmt.Errorf("scan console session: %w", err)
	}
	if closedAt.Valid {
		item.ClosedAt = &closedAt.String
	}
	return item, nil
}

func limitConsoleTranscript(value string) string {
	if len(value) <= maxConsoleTranscriptLength {
		return value
	}
	return terminaltext.TailStringByBytes(value, maxConsoleTranscriptLength)
}
