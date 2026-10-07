package retention

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
	"github.com/aipermission/aipermission/backend/internal/retention/sqlstore"
	"github.com/aipermission/aipermission/backend/internal/sqldb"
	"github.com/aipermission/aipermission/backend/internal/transactionstate"
)

var ErrMutationRunnerRequired = errors.New("retention mutation runner is required")
var errNoRetainedRecords = errors.New("no retained records removed")

type Service struct {
	database       *sql.DB
	repository     repository
	workspaceID    string
	interval       time.Duration
	operationMu    sync.Mutex
	lifecycleMu    sync.Mutex
	cancel         context.CancelFunc
	done           chan struct{}
	automaticAudit auditedmutation.Runner
}

func NewService(database *sql.DB, workspaceID string, automaticAudit auditedmutation.Runner) *Service {
	return &Service{database: database, repository: sqlstore.Store{}, workspaceID: workspaceID, interval: defaultCleanupInterval, automaticAudit: automaticAudit}
}

func (s *Service) Read(ctx context.Context) (Settings, error) {
	if !s.available() {
		return Settings{}, errors.New("retention database is unavailable")
	}
	return readSettings(ctx, s.database, s.repository)
}

func (s *Service) Update(ctx context.Context, settings Settings, mutate auditedmutation.Runner) (map[string]int64, error) {
	if !s.available() {
		return nil, errors.New("retention database is unavailable")
	}
	if err := ValidateSettings(settings); err != nil {
		return nil, err
	}
	if mutate == nil {
		return nil, ErrMutationRunnerRequired
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	deleted := map[string]int64{}
	err := mutate(ctx, "settings.retention.updated", func() any {
		return map[string]any{
			"history_days": settings.HistoryDays, "audit_days": settings.AuditDays,
			"console_days": settings.ConsoleDays, "message_days": settings.MessageDays,
			"deleted": deleted,
		}
	}, func(tx *sql.Tx) error {
		if err := writeSettings(ctx, tx, s.repository, settings); err != nil {
			return err
		}
		var err error
		deleted, err = applySettings(ctx, tx, s.repository, settings)
		return err
	})
	return deleted, err
}

func (s *Service) PurgeAudited(ctx context.Context, target string, days int, mutate auditedmutation.Runner) (int64, error) {
	if !s.available() {
		return 0, errors.New("retention database is unavailable")
	}
	if days < 1 {
		return 0, ValidationError("days must be at least 1")
	}
	if mutate == nil {
		return 0, ErrMutationRunnerRequired
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	var deleted int64
	err := mutate(ctx, "settings.retention.purged", func() any {
		return map[string]any{"target": target, "days": days, "deleted": deleted}
	}, func(tx *sql.Tx) error {
		var err error
		deleted, err = purgeTarget(ctx, tx, s.repository, target, days)
		return err
	})
	return deleted, err
}

func (s *Service) purge(ctx context.Context, target string, days int) (int64, error) {
	if !s.available() {
		return 0, errors.New("retention database is unavailable")
	}
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	return s.purgeInTransaction(ctx, target, days)
}

func (s *Service) available() bool {
	return s != nil && s.database != nil && s.repository != nil
}

func (s *Service) purgeInTransaction(ctx context.Context, target string, days int) (int64, error) {
	executor, commit, rollback, err := sqldb.Transaction(ctx, s.database, nil, "retention purge")
	if err != nil {
		return 0, err
	}
	defer rollback()
	deleted, err := purgeTarget(ctx, executor, s.repository, target, days)
	if err != nil {
		return 0, err
	}
	if err := commit(); err != nil {
		return 0, fmt.Errorf("commit retention purge: %w", err)
	}
	return deleted, nil
}

func (s *Service) applyConfigured(ctx context.Context) (map[string]int64, error) {
	if s.automaticAudit == nil {
		return nil, ErrMutationRunnerRequired
	}
	settings, err := readSettings(ctx, s.database, s.repository)
	if err != nil {
		return nil, err
	}
	var deleted map[string]int64
	err = s.automaticAudit(ctx, "settings.retention.automatic_cleanup", func() any {
		return map[string]any{"deleted": deleted, "history_days": settings.HistoryDays, "audit_days": settings.AuditDays, "console_days": settings.ConsoleDays, "message_days": settings.MessageDays}
	}, func(tx *sql.Tx) error {
		var err error
		deleted, err = applySettings(ctx, tx, s.repository, settings)
		if err != nil {
			return err
		}
		for _, count := range deleted {
			if count > 0 {
				return nil
			}
		}
		// Abort this no-op transaction so the audit runner does not append an event.
		return errNoRetainedRecords
	})
	if errors.Is(err, errNoRetainedRecords) && transactionstate.IsNotCommitted(err) {
		return deleted, nil
	}
	if err != nil {
		return nil, err
	}
	return deleted, nil
}
