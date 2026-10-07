// Package capacityreport shapes local-operator capacity observations.
package capacityreport

import (
	"context"
	"strconv"

	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

type TokenReport struct {
	TokenID       string `json:"token_id"`
	Name          string `json:"name"`
	Rows          int64  `json:"rows"`
	StoredBytes   int64  `json:"stored_bytes"`
	ReservedBytes int64  `json:"reserved_bytes"`
	Running       int64  `json:"running"`
	Pending       int64  `json:"pending"`
	Level         string `json:"level"`
}

type Report struct {
	Items                       []TokenReport `json:"items"`
	RowLimit                    int64         `json:"row_limit"`
	ByteLimit                   int64         `json:"byte_limit"`
	RunningLimit                int64         `json:"running_limit"`
	NextRequestReservationBytes int64         `json:"next_request_reservation_bytes"`
}

func StorageLevel(usage actioncapacity.Usage, limits actioncapacity.Limits) string {
	if usage.Rows+1 > limits.Rows || usage.Bytes+actioncapacity.TerminalReservationBytes > limits.Bytes {
		return "exhausted"
	}
	if usage.Rows*100 >= limits.Rows*90 || usage.Bytes*100 >= limits.Bytes*90 {
		return "critical"
	}
	if usage.Rows*100 >= limits.Rows*80 || usage.Bytes*100 >= limits.Bytes*80 {
		return "warning"
	}
	return "ok"
}

// ReadReport returns local-operator usage, never token values or request content.
func ReadReport(ctx context.Context, executor sqldb.Executor, limits actioncapacity.Limits) (Report, error) {
	report := Report{Items: []TokenReport{}, RowLimit: limits.Rows, ByteLimit: limits.Bytes,
		RunningLimit: limits.Running, NextRequestReservationBytes: actioncapacity.TerminalReservationBytes}
	rows, err := executor.QueryContext(ctx, `SELECT t.id, t.name, COUNT(u.request_id),
	 COALESCE(SUM(u.stored_bytes),0),
	 COALESCE(SUM(CASE WHEN u.status='running' THEN 1 ELSE 0 END),0),
	 COALESCE(SUM(CASE WHEN u.status='approval_pending' THEN 1 ELSE 0 END),0)
	 FROM api_tokens t LEFT JOIN connector_action_request_usage u ON u.token_id=t.id
	 WHERE t.revoked_at IS NULL GROUP BY t.id, t.name ORDER BY t.name, t.id`)
	if err != nil {
		return Report{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item TokenReport
		var id int64
		if err := rows.Scan(&id, &item.Name, &item.Rows, &item.StoredBytes, &item.Running, &item.Pending); err != nil {
			return Report{}, err
		}
		item.TokenID = strconv.FormatInt(id, 10)
		item.ReservedBytes = (item.Running + item.Pending) * actioncapacity.TerminalReservationBytes
		item.Level = StorageLevel(actioncapacity.Usage{Rows: item.Rows, Bytes: item.StoredBytes + item.ReservedBytes, Running: item.Running}, limits)
		report.Items = append(report.Items, item)
	}
	return report, rows.Err()
}
