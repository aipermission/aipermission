package securitypolicy

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSettingsMutationCommitsStateTokenCleanupAndAuditAtomically(t *testing.T) {
	database := openTestDatabase(t)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, token_value, created_at, updated_at)
		VALUES ('agent', 'hash', 'prefix', 'reusable-secret', ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	service := NewService(database)

	settings, err := service.UpdateSettings(t.Context(), Settings{
		ExposeMCPServerMetadata: true,
		MCPStartEnabled:         true,
		RedactionMode:           "invalid",
	}, auditRunner(database, nil))
	if err != nil {
		t.Fatal(err)
	}
	if settings.ReusableTokens || !settings.ExposeMCPServerMetadata || !settings.MCPStartEnabled || settings.RedactionMode != RedactionModeBasic {
		t.Fatalf("settings = %#v", settings)
	}
	var tokenValue string
	if err := database.QueryRow(`SELECT token_value FROM api_tokens WHERE name = 'agent'`).Scan(&tokenValue); err != nil {
		t.Fatal(err)
	}
	if tokenValue != "" {
		t.Fatal("disabling reusable tokens did not clear persisted token material")
	}
	if countRows(t, database, "audit_logs") != 1 {
		t.Fatal("settings mutation and audit were not committed together")
	}
}

func TestSettingsMutationRollbackDoesNotPoisonCache(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database)
	baseline := Settings{ReusableTokens: true, RedactionMode: RedactionModeOff}
	if _, err := service.UpdateSettings(t.Context(), baseline, auditRunner(database, nil)); err != nil {
		t.Fatal(err)
	}
	forced := errors.New("forced audit failure")
	if _, err := service.UpdateSettings(t.Context(), Settings{RedactionMode: RedactionModeBasic}, auditRunner(database, forced)); !errors.Is(err, forced) {
		t.Fatalf("update error = %v", err)
	}
	settings, err := service.ReadSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if settings != baseline {
		t.Fatalf("rollback changed cached settings: got %#v want %#v", settings, baseline)
	}
	if countRows(t, database, "audit_logs") != 1 {
		t.Fatal("failed mutation persisted an audit event")
	}
}

func TestRuleLifecycleInvalidatesCompiledCacheAndAuditsMutations(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database)
	runner := auditRunner(database, nil)
	item, err := service.CreateRule(t.Context(), RuleInput{Name: "first", Pattern: `alpha_[a-z0-9]+`, Enabled: true}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if value := service.Redact(t.Context(), "alpha_secret beta_secret"); strings.Contains(value, "alpha_secret") {
		t.Fatalf("created rule was not applied: %s", value)
	}
	if _, err := service.UpdateRule(t.Context(), item.ID, RuleInput{Name: "second", Pattern: `beta_[a-z0-9]+`, Enabled: true}, runner); err != nil {
		t.Fatal(err)
	}
	value := service.Redact(t.Context(), "alpha_secret beta_secret")
	if !strings.Contains(value, "alpha_secret") || strings.Contains(value, "beta_secret") {
		t.Fatalf("updated rule cache was not refreshed: %s", value)
	}
	if err := service.DeleteRule(t.Context(), item.ID, runner); err != nil {
		t.Fatal(err)
	}
	if value := service.Redact(t.Context(), "beta_secret"); !strings.Contains(value, "beta_secret") {
		t.Fatalf("deleted rule remained cached: %s", value)
	}
	if countRows(t, database, "audit_logs") != 3 {
		t.Fatal("rule lifecycle did not persist one audit event per mutation")
	}
}

func TestRuleMutationRollbackKeepsCommittedRuleAndCache(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database)
	runner := auditRunner(database, nil)
	item, err := service.CreateRule(t.Context(), RuleInput{Name: "first", Pattern: `alpha_[a-z0-9]+`, Enabled: true}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if value := service.Redact(t.Context(), "alpha_secret"); strings.Contains(value, "alpha_secret") {
		t.Fatalf("initial rule was not cached: %s", value)
	}

	forced := errors.New("forced audit failure")
	if _, err := service.UpdateRule(t.Context(), item.ID, RuleInput{Name: "second", Pattern: `beta_[a-z0-9]+`, Enabled: true}, auditRunner(database, forced)); !errors.Is(err, forced) {
		t.Fatalf("update error = %v", err)
	}
	value := service.Redact(t.Context(), "alpha_secret beta_secret")
	if strings.Contains(value, "alpha_secret") || !strings.Contains(value, "beta_secret") {
		t.Fatalf("rollback changed committed rule or cache: %s", value)
	}
	rules, err := service.ListRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Name != "first" || rules[0].Pattern != `alpha_[a-z0-9]+` {
		t.Fatalf("rollback changed persisted rule: %#v", rules)
	}
	if countRows(t, database, "audit_logs") != 1 {
		t.Fatal("failed rule mutation persisted an audit event")
	}
}

func TestRuleCreateAndDeleteRollbackKeepCommittedState(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database)
	forced := errors.New("forced audit failure")
	if _, err := service.CreateRule(t.Context(), RuleInput{Name: "rolled-back", Pattern: "never", Enabled: true}, auditRunner(database, forced)); !errors.Is(err, forced) {
		t.Fatalf("create error = %v", err)
	}
	if countRows(t, database, "redaction_rules") != 0 || countRows(t, database, "audit_logs") != 0 {
		t.Fatal("failed create persisted rule or audit state")
	}

	runner := auditRunner(database, nil)
	item, err := service.CreateRule(t.Context(), RuleInput{Name: "kept", Pattern: `kept_[a-z]+`, Enabled: true}, runner)
	if err != nil {
		t.Fatal(err)
	}
	if value := service.Redact(t.Context(), "kept_secret"); strings.Contains(value, "kept_secret") {
		t.Fatalf("created rule was not cached: %s", value)
	}
	if err := service.DeleteRule(t.Context(), item.ID, auditRunner(database, forced)); !errors.Is(err, forced) {
		t.Fatalf("delete error = %v", err)
	}
	if value := service.Redact(t.Context(), "kept_secret"); strings.Contains(value, "kept_secret") {
		t.Fatalf("failed delete changed compiled cache: %s", value)
	}
	rules, err := service.ListRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != item.ID {
		t.Fatalf("failed delete changed persisted rules: %#v", rules)
	}
	if countRows(t, database, "audit_logs") != 1 {
		t.Fatal("failed delete persisted an audit event")
	}
}

func TestConcurrentRuleCreationCannotExceedLimit(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database)
	runner := auditRunner(database, nil)
	for index := 0; index < maxRules-1; index++ {
		if _, err := service.CreateRule(t.Context(), RuleInput{
			Name: fmt.Sprintf("rule-%02d", index), Pattern: fmt.Sprintf(`value_%02d`, index), Enabled: true,
		}, runner); err != nil {
			t.Fatal(err)
		}
	}

	var group sync.WaitGroup
	errorsSeen := make(chan error, 2)
	for index := 0; index < 2; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			_, err := service.CreateRule(t.Context(), RuleInput{
				Name: fmt.Sprintf("last-%d", index), Pattern: fmt.Sprintf(`last_%d`, index), Enabled: true,
			}, runner)
			errorsSeen <- err
		}(index)
	}
	group.Wait()
	close(errorsSeen)
	successes, rejected := 0, 0
	for err := range errorsSeen {
		switch {
		case err == nil:
			successes++
		case strings.Contains(err.Error(), "redaction rule limit"):
			rejected++
		default:
			t.Fatalf("unexpected concurrent create error: %v", err)
		}
	}
	if successes != 1 || rejected != 1 || countRows(t, database, "redaction_rules") != maxRules {
		t.Fatalf("limit result: successes=%d rejected=%d rows=%d", successes, rejected, countRows(t, database, "redaction_rules"))
	}
}

func TestMissingServiceRedactorFailsSafe(t *testing.T) {
	var service *Service
	if value := service.PrepareRedactor(t.Context())("password=secret"); strings.Contains(value, "secret") {
		t.Fatalf("missing service exposed a basic secret: %s", value)
	}
}

func TestCustomRuleReadFailureRedactsWholeValue(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database)
	if _, err := service.ReadSettings(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	const value = "custom-secret-without-basic-shape"
	if redacted := service.Redact(t.Context(), value); redacted != redactionFailureMarker {
		t.Fatalf("runtime redaction failure returned %q", redacted)
	}
	if redacted := service.PrepareRedactor(t.Context())(value); redacted != redactionFailureMarker {
		t.Fatalf("prepared redaction failure returned %q", redacted)
	}
}
