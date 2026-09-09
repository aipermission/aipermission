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
)

var ErrMutationRunnerRequired = errors.New("retention mutation runner is required")

type Service struct {
	database    *sql.DB
	repository  repository
	workspaceID string
	interval    time.Duration
	operationMu sync.Mutex
	lifecycleMu sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
}

func NewService(database *sql.DB, workspaceID string) *Service {
	return &Service{database: database, repository: sqlstore.Store{}, workspaceID: workspaceID, interval: defaultCleanupInterval}
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
	settings, err := readSettings(ctx, s.database, s.repository)
	if err != nil {
		return nil, err
	}
	executor, commit, rollback, err := sqldb.Transaction(ctx, s.database, nil, "retention settings")
	if err != nil {
		return nil, err
	}
	defer rollback()
	deleted, err := applySettings(ctx, executor, s.repository, settings)
	if err != nil {
		return nil, err
	}
	if err := commit(); err != nil {
		return nil, fmt.Errorf("commit retention settings: %w", err)
	}
	return deleted, nil
}
