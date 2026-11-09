// Package idlecleanup owns opt-in, workspace-local console cleanup scheduling.
package idlecleanup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console/capacity"
)

const policyKey = "console_idle_cleanup"

type Policy = capacity.CleanupPolicy

type Ports struct {
	Snapshot func(context.Context) (capacity.Report, error)
	Close    func(context.Context, capacity.Selection, int) (bool, error)
	Observe  func(context.Context, string, any) error
}

type Service struct {
	database  *sql.DB
	ports     Ports
	operation sync.Mutex
	once      sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
}

func New(database *sql.DB, ports Ports) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{database: database, ports: ports, ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

func (s *Service) Read(ctx context.Context) (Policy, error) {
	var value string
	err := s.database.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, policyKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return Policy{IdleMinutes: 60}, nil
	}
	if err != nil {
		return Policy{}, err
	}
	var policy Policy
	if json.Unmarshal([]byte(value), &policy) != nil || !policy.Valid() {
		return Policy{}, errors.New("invalid console cleanup policy")
	}
	return policy, nil
}

func (s *Service) Update(ctx context.Context, policy Policy, mutate capacity.MutationRunner) error {
	if !policy.Valid() || mutate == nil {
		return errors.New("invalid console cleanup policy update")
	}
	s.operation.Lock()
	defer s.operation.Unlock()
	value, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	return mutate(ctx, "console.cleanup.policy_updated", func() any { return policy }, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, policyKey, string(value), time.Now().UTC().Format(time.RFC3339Nano))
		return err
	})
}

// Cleanup is serialized with policy changes. It never scans historical rows.
func (s *Service) Cleanup(ctx context.Context) error {
	if !s.operation.TryLock() {
		return nil
	}
	defer s.operation.Unlock()
	policy, err := s.Read(ctx)
	if err != nil || !policy.Enabled {
		return err
	}
	if s.ports.Snapshot == nil || s.ports.Close == nil || s.ports.Observe == nil {
		return errors.New("console cleanup ports unavailable")
	}
	report, err := s.ports.Snapshot(ctx)
	if err != nil {
		return err
	}
	items := []capacity.Selection{}
	for _, item := range report.Items {
		if item.Eligible(time.Now(), policy.IdleMinutes) {
			items = append(items, capacity.Selection{ID: item.ID, Generation: item.Generation, LastActivity: item.LastActivity})
		}
	}
	if len(items) == 0 {
		return nil
	}
	if len(items) > capacity.Limit {
		return errors.New("console cleanup snapshot exceeds limit")
	}
	if err := s.ports.Observe(ctx, "console.cleanup.automatic_started", map[string]any{"sessions": items, "idle_minutes": policy.IdleMinutes}); err != nil {
		return err
	}
	result := capacity.CloseResult{Closed: []int64{}, Skipped: []int64{}, Uncertain: []int64{}}
	for _, item := range items {
		if ctx.Err() != nil {
			result.Skipped = append(result.Skipped, item.ID)
			continue
		}
		closed, err := s.ports.Close(ctx, item, policy.IdleMinutes)
		switch {
		case err != nil:
			result.Uncertain = append(result.Uncertain, item.ID)
		case closed:
			result.Closed = append(result.Closed, item.ID)
		default:
			result.Skipped = append(result.Skipped, item.ID)
		}
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.ports.Observe(auditCtx, "console.cleanup.automatic_completed", result); err != nil {
		return fmt.Errorf("record console cleanup outcome: %w", err)
	}
	return ctx.Err()
}
