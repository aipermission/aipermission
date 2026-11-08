// Package capacity defines bounded, transcript-free console capacity contracts.
package capacity

import "time"

const Limit = 32

type Session struct {
	ID            int64  `json:"id"`
	RuntimeID     int64  `json:"runtime_id"`
	Generation    int64  `json:"generation"`
	TargetName    string `json:"target_name"`
	ConnectorKind string `json:"connector_kind"`
	Name          string `json:"name"`
	Owner         string `json:"owner"`
	State         string `json:"state"`
	Clients       int    `json:"clients"`
	LastActivity  string `json:"last_activity"`
	CanClose      bool   `json:"can_close"`
}

type Report struct {
	Limit      int       `json:"limit"`
	Used       int       `json:"used"`
	Available  int       `json:"available"`
	Revision   uint64    `json:"revision"`
	ObservedAt string    `json:"observed_at"`
	Items      []Session `json:"items"`
}

type Selection struct {
	ID           int64  `json:"id"`
	Generation   int64  `json:"generation"`
	LastActivity string `json:"last_activity"`
}

type CloseRequest struct {
	IdleMinutes *int        `json:"idle_minutes"`
	Sessions    []Selection `json:"sessions"`
}

type CloseResult struct {
	Closed    []int64 `json:"closed"`
	Skipped   []int64 `json:"skipped"`
	Uncertain []int64 `json:"uncertain"`
}

func ValidIdleMinutes(minutes int) bool {
	switch minutes {
	case 0, 10, 20, 30, 45, 60, 120, 180, 360:
		return true
	default:
		return false
	}
}

func (session Session) Eligible(now time.Time, minutes int) bool {
	last, err := time.Parse(time.RFC3339Nano, session.LastActivity)
	return session.CanClose && err == nil && !last.After(now) && now.Sub(last) >= time.Duration(minutes)*time.Minute
}
