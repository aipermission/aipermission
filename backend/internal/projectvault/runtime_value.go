package projectvault

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const previewTTL = 5 * time.Minute

type ReplaceRuntimeValueInput struct {
	ID                   int64
	Value                string
	Source               string
	GeneratorKind        string
	PreviewToken         string
	ExpectedValueVersion int64
}

type GeneratedPreview struct {
	ItemID               int64          `json:"item_id"`
	ExpectedValueVersion int64          `json:"expected_value_version"`
	GeneratorKind        string         `json:"generator_kind"`
	GeneratorParameters  map[string]any `json:"generator_parameters"`
	Value                string         `json:"value"`
	ExpiresAtUnix        int64          `json:"expires_at_unix"`
	Nonce                string         `json:"nonce"`
}

type GeneratedPreviewResult struct {
	Value         string
	PreviewToken  string
	GeneratorKind string
	ExpiresAt     time.Time
}

func (r *Runtime) ReplaceValue(ctx context.Context, input ReplaceRuntimeValueInput) (Item, error) {
	if err := r.validateMutation(); err != nil {
		return Item{}, err
	}
	input.Source = strings.TrimSpace(input.Source)
	input.GeneratorKind = strings.TrimSpace(input.GeneratorKind)
	if input.Source == "" {
		input.Source = "imported"
	}
	if input.Source == "generated" {
		if input.Value != "" {
			return Item{}, ValidationError("generated replacement cannot include an imported value")
		}
		if input.PreviewToken == "" {
			return Item{}, ValidationError("generated replacement preview is required")
		}
		if err := ValidateGeneratorKind(input.GeneratorKind); err != nil {
			return Item{}, err
		}
	} else if input.Source != "imported" || input.GeneratorKind != "" || input.PreviewToken != "" {
		return Item{}, ValidationError("source must select an imported value or a supported generator")
	}
	release, err := r.delivery.AcquireExclusive(ctx)
	if err != nil {
		return Item{}, err
	}
	defer release()
	current, err := r.store.Get(ctx, input.ID)
	if err != nil {
		return Item{}, err
	}
	if current.ValueVersion != input.ExpectedValueVersion {
		return Item{}, ErrStale
	}
	var preview GeneratedPreview
	if input.Source == "generated" {
		if r.store.vault.DecryptJSONWithAAD(input.PreviewToken, &preview, r.previewAAD(input.ID, input.ExpectedValueVersion)) != nil ||
			preview.ItemID != input.ID || preview.ExpectedValueVersion != input.ExpectedValueVersion ||
			preview.GeneratorKind != input.GeneratorKind || preview.ExpiresAtUnix <= r.now().UTC().Unix() ||
			!r.previewCurrent(input.ID, preview.Nonce) {
			return Item{}, ValidationError("generated replacement preview is invalid or expired")
		}
		input.Value = preview.Value
	}
	scope := SessionMutationScope{ItemID: input.ID}
	sessions, err := r.store.ActiveSessionsForMutation(ctx, scope)
	if err != nil {
		return Item{}, err
	}
	storeInput := ReplaceValueInput{
		ID: input.ID, Value: input.Value, Source: input.Source, GeneratorKind: input.GeneratorKind,
		GeneratorParams: preview.GeneratorParameters, ExpectedValueVersion: input.ExpectedValueVersion,
	}
	var item Item
	err = r.mutations.WithMutation(ctx, "vault.item.value_replaced", func() any {
		return ItemAuditPayload(item)
	}, func(tx *sql.Tx) error {
		var replaceErr error
		item, replaceErr = r.store.WithTx(tx).ReplaceValue(ctx, storeInput)
		return replaceErr
	})
	if err != nil {
		return Item{}, err
	}
	if err := r.invalidateSessions(ctx, sessions, scope); err != nil {
		return Item{}, err
	}
	r.clearPreview(input.ID)
	return item, nil
}

func (r *Runtime) GeneratePreview(ctx context.Context, id int64, generatorKind, rateKey string) (GeneratedPreviewResult, error) {
	if r == nil || r.store == nil || r.delivery == nil || r.mutations == nil || r.allowGenerate == nil || r.nonce == nil {
		return GeneratedPreviewResult{}, ErrRuntimeUnavailable
	}
	generatorKind = strings.TrimSpace(generatorKind)
	if err := ValidateGeneratorKind(generatorKind); err != nil {
		return GeneratedPreviewResult{}, err
	}
	release, err := r.delivery.AcquireDelivery(ctx)
	if err != nil {
		return GeneratedPreviewResult{}, err
	}
	defer release()
	item, err := r.store.Get(ctx, id)
	if err != nil {
		return GeneratedPreviewResult{}, err
	}
	if !r.allowGenerate(rateKey) {
		return GeneratedPreviewResult{}, ErrGenerateRateLimited
	}
	value, parameters, err := Generate(generatorKind)
	if err != nil {
		return GeneratedPreviewResult{}, err
	}
	expiresAt := r.now().UTC().Add(previewTTL)
	nonce, err := r.nonce()
	if err != nil {
		return GeneratedPreviewResult{}, err
	}
	preview := GeneratedPreview{
		ItemID: id, ExpectedValueVersion: item.ValueVersion, GeneratorKind: generatorKind,
		GeneratorParameters: parameters, Value: value, ExpiresAtUnix: expiresAt.Unix(), Nonce: nonce,
	}
	token, err := r.store.vault.EncryptJSONWithAAD(preview, r.previewAAD(id, item.ValueVersion))
	if err != nil {
		return GeneratedPreviewResult{}, err
	}
	if err := r.mutations.Observe(ctx, "vault.item.value_preview.generated", map[string]any{
		"project_id": item.OwnerProjectID, "vault_item_id": id,
		"expected_value_version": item.ValueVersion, "generator_kind": generatorKind,
		"expires_at": expiresAt.Format(time.RFC3339),
	}); err != nil {
		return GeneratedPreviewResult{}, err
	}
	r.setPreview(id, nonce)
	return GeneratedPreviewResult{Value: value, PreviewToken: token, GeneratorKind: generatorKind, ExpiresAt: expiresAt}, nil
}

func (r *Runtime) Reveal(ctx context.Context, id int64, rateKey string) (string, error) {
	if r == nil || r.store == nil || r.delivery == nil || r.mutations == nil || r.allowReveal == nil {
		return "", ErrRuntimeUnavailable
	}
	if !r.allowReveal(rateKey) {
		return "", ErrRevealRateLimited
	}
	release, err := r.delivery.AcquireDelivery(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	item, err := r.store.Get(ctx, id)
	if err != nil {
		return "", err
	}
	value, err := r.store.Reveal(ctx, id)
	if err != nil {
		return "", err
	}
	if err := r.mutations.Observe(ctx, "vault.item.revealed", ItemAuditPayload(item)); err != nil {
		return "", err
	}
	return value, nil
}

func (r *Runtime) previewAAD(itemID, valueVersion int64) []byte {
	return []byte(fmt.Sprintf("project-vault-value-preview:v1:%s:%d:%d", r.store.workspaceUUID, itemID, valueVersion))
}

func (r *Runtime) setPreview(itemID int64, nonce string) {
	r.previewMu.Lock()
	r.previewNonces[itemID] = nonce
	r.previewMu.Unlock()
}

func (r *Runtime) previewCurrent(itemID int64, nonce string) bool {
	if nonce == "" {
		return false
	}
	r.previewMu.Lock()
	defer r.previewMu.Unlock()
	return r.previewNonces[itemID] == nonce
}

func (r *Runtime) clearPreview(itemID int64) {
	r.previewMu.Lock()
	delete(r.previewNonces, itemID)
	r.previewMu.Unlock()
}
