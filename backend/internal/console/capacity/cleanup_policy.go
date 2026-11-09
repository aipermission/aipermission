package capacity

import "github.com/aipermission/aipermission/backend/internal/auditedmutation"

// MutationRunner is the existing atomic settings-and-audit boundary.
type MutationRunner = auditedmutation.Runner

type CleanupPolicy struct {
	Enabled     bool `json:"enabled"`
	IdleMinutes int  `json:"idle_minutes"`
}

func (p CleanupPolicy) Valid() bool { return p.IdleMinutes > 0 && ValidIdleMinutes(p.IdleMinutes) }
