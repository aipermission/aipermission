package actions

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type nativeWorkflowTokens struct{ store *tokens.Store }

func (reader nativeWorkflowTokens) Get(ctx context.Context, id int64, now time.Time) (AuthorizationToken, error) {
	token, err := reader.store.Get(ctx, id)
	if errors.Is(err, tokens.ErrNotFound) {
		return AuthorizationToken{}, ErrTokenNotFound
	}
	if err != nil {
		return AuthorizationToken{}, err
	}
	return AuthorizationToken{ID: token.ID, Active: tokens.Active(token.RevokedAt, token.ExpiresAt, now), RevokedAt: token.RevokedAt, ExpiresAt: token.ExpiresAt}, nil
}

type nativeWorkflowTargets struct{ store *connectortargets.Store }

func (resolver nativeWorkflowTargets) ResolveActionTarget(ctx context.Context, ref string) (ResolvedTarget, error) {
	target, profile, err := resolver.store.ResolveConnectorActionTarget(ctx, ref)
	return ResolvedTarget{Target: target, Profile: profile}, err
}

type nativeWorkflowRecords struct{ vault *vault.Vault }

func (records nativeWorkflowRecords) SealActionRequest(id int64, envelope ExecutionEnvelope) (string, error) {
	return recordcrypto.EncryptJSON(records.vault, "native-workflow", recordcrypto.ConnectorActionRequest, id, envelope)
}

func (records nativeWorkflowRecords) OpenActionRequest(id int64, sealed string) (ExecutionEnvelope, error) {
	var envelope ExecutionEnvelope
	err := recordcrypto.DecryptJSON(records.vault, "native-workflow", recordcrypto.ConnectorActionRequest, id, sealed, &envelope)
	return envelope, err
}

func (records nativeWorkflowRecords) OpenCredentialProfile(id int64, sealed string) (map[string]any, error) {
	secrets := map[string]any{}
	err := recordcrypto.DecryptJSON(records.vault, "native-workflow", recordcrypto.ConnectorCredentialProfile, id, sealed, &secrets)
	return secrets, err
}

type nativeWorkflowMutations struct {
	*observability.Coordinator
	t *testing.T
}

func (port nativeWorkflowMutations) WithTransaction(ctx context.Context, mutate func(*sql.Tx, AuditAppender) error) error {
	return port.Coordinator.WithTransaction(ctx, func(tx *sql.Tx, appendAudit observability.Appender) error {
		return mutate(tx, AuditAppender(appendAudit))
	})
}

func (port nativeWorkflowMutations) Observe(ctx context.Context, actor string, tokenID *int64, runtimeID int64, action string, payload any) {
	// Required admission/finality audit goes through transactions. The best-effort
	// observation port is still backed by the same actual coordinator.
	if err := port.Coordinator.WriteRequired(ctx, actor, tokenID, runtimeID, action, payload); err != nil {
		port.t.Errorf("native workflow observation %s: %v", action, err)
	}
}
