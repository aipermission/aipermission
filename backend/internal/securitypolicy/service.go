package securitypolicy

import (
	"context"
	"database/sql"
	"errors"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/auditedmutation"
)

var ErrMutationRunnerRequired = errors.New("security policy mutation runner is required")

type Service struct {
	database *sql.DB

	mutationMu sync.Mutex
	settingsMu sync.RWMutex
	settings   Settings
	loaded     bool

	rulesMu     sync.RWMutex
	compiled    []compiledRule
	rulesLoaded bool
}

func NewService(database *sql.DB) *Service {
	return &Service{database: database}
}

func (s *Service) ReadSettings(ctx context.Context) (Settings, error) {
	if s == nil || s.database == nil {
		return Settings{}, errors.New("security policy database is unavailable")
	}
	s.settingsMu.RLock()
	if s.loaded {
		settings := s.settings
		s.settingsMu.RUnlock()
		return settings, nil
	}
	s.settingsMu.RUnlock()

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.loaded {
		return s.settings, nil
	}
	settings, err := readSettings(ctx, s.database)
	if err != nil {
		return Settings{}, err
	}
	s.settings, s.loaded = settings, true
	return settings, nil
}

func (s *Service) UpdateSettings(ctx context.Context, settings Settings, mutate auditedmutation.Runner) (Settings, error) {
	if s == nil || s.database == nil {
		return Settings{}, errors.New("security policy database is unavailable")
	}
	if mutate == nil {
		return Settings{}, ErrMutationRunnerRequired
	}
	settings = NormalizeSettings(settings)
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	err := mutate(ctx, "settings.security.updated", func() any {
		return settingsAuditPayload(settings)
	}, func(tx *sql.Tx) error {
		return writeSettings(ctx, tx, settings)
	})
	if err != nil {
		return Settings{}, err
	}
	s.settingsMu.Lock()
	s.settings, s.loaded = settings, true
	s.settingsMu.Unlock()
	return settings, nil
}

func (s *Service) ListRules(ctx context.Context) ([]Rule, error) {
	if s == nil || s.database == nil {
		return nil, errors.New("security policy database is unavailable")
	}
	return listRules(ctx, s.database, false)
}

func (s *Service) CreateRule(ctx context.Context, input RuleInput, mutate auditedmutation.Runner) (Rule, error) {
	input = normalizeRuleInput(input)
	if err := validateMutation(s, mutate, input); err != nil {
		return Rule{}, err
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	var item Rule
	err := mutate(ctx, "settings.redaction_rule.created", func() any { return ruleAuditPayload(item) }, func(tx *sql.Tx) error {
		var err error
		item, err = insertRule(ctx, tx, input)
		return err
	})
	if err != nil {
		return Rule{}, err
	}
	s.invalidateRules()
	return item, nil
}

func (s *Service) UpdateRule(ctx context.Context, id int64, input RuleInput, mutate auditedmutation.Runner) (Rule, error) {
	input = normalizeRuleInput(input)
	if id < 1 {
		return Rule{}, ValidationError("invalid id")
	}
	if err := validateMutation(s, mutate, input); err != nil {
		return Rule{}, err
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	var item Rule
	err := mutate(ctx, "settings.redaction_rule.updated", func() any { return ruleAuditPayload(item) }, func(tx *sql.Tx) error {
		var err error
		item, err = updateRule(ctx, tx, id, input)
		return err
	})
	if err != nil {
		return Rule{}, err
	}
	s.invalidateRules()
	return item, nil
}

func (s *Service) DeleteRule(ctx context.Context, id int64, mutate auditedmutation.Runner) error {
	if s == nil || s.database == nil {
		return errors.New("security policy database is unavailable")
	}
	if id < 1 {
		return ValidationError("invalid id")
	}
	if mutate == nil {
		return ErrMutationRunnerRequired
	}
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if err := mutate(ctx, "settings.redaction_rule.deleted", func() any {
		return map[string]any{"id": id}
	}, func(tx *sql.Tx) error {
		return deleteRule(ctx, tx, id)
	}); err != nil {
		return err
	}
	s.invalidateRules()
	return nil
}

func validateMutation(service *Service, mutate auditedmutation.Runner, input RuleInput) error {
	if service == nil || service.database == nil {
		return errors.New("security policy database is unavailable")
	}
	if mutate == nil {
		return ErrMutationRunnerRequired
	}
	return validateRuleInput(input)
}

func settingsAuditPayload(settings Settings) map[string]any {
	return map[string]any{
		"reusable_tokens": settings.ReusableTokens, "expose_mcp_server_metadata": settings.ExposeMCPServerMetadata,
		"mcp_start_enabled": settings.MCPStartEnabled, "redaction_mode": settings.RedactionMode,
	}
}

func ruleAuditPayload(rule Rule) map[string]any {
	return map[string]any{"id": rule.ID, "name": rule.Name, "enabled": rule.Enabled}
}

func (s *Service) invalidateRules() {
	s.rulesMu.Lock()
	s.compiled, s.rulesLoaded = nil, false
	s.rulesMu.Unlock()
}
