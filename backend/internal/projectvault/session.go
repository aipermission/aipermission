package projectvault

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

type SessionSelection struct {
	ItemID          int64 `json:"item_id"`
	SourceProjectID int64 `json:"source_project_id"`
	ReplaceExisting bool  `json:"replace_existing"`
	BindingID       int64 `json:"binding_id,omitempty"`
	BindingRevision int64 `json:"binding_revision,omitempty"`
}

type SessionItem struct {
	ItemID           int64  `json:"item_id"`
	Name             string `json:"name"`
	SourceProjectID  int64  `json:"source_project_id"`
	ValueVersion     int64  `json:"value_version"`
	MetadataRevision int64  `json:"metadata_revision"`
	ReplaceExisting  bool   `json:"replace_existing"`
	BindingID        int64  `json:"binding_id,omitempty"`
	BindingRevision  int64  `json:"binding_revision,omitempty"`
}

type SessionResolution struct {
	Items       []SessionItem
	Environment *sessionenv.Envelope
	ContentHash string
}

func (r *SessionResolution) Destroy() {
	if r != nil && r.Environment != nil {
		r.Environment.Destroy()
		r.Environment = nil
	}
}

func (s *Store) ResolveSession(ctx context.Context, selections []SessionSelection) (SessionResolution, error) {
	return s.resolveSession(ctx, selections, true)
}

func (s *Store) SnapshotSession(ctx context.Context, selections []SessionSelection) (SessionResolution, error) {
	return s.resolveSession(ctx, selections, false)
}

func (s *Store) resolveSession(ctx context.Context, selections []SessionSelection, includeValues bool) (SessionResolution, error) {
	if len(selections) == 0 {
		contentHash, err := sessionContentHash(nil)
		if err != nil {
			return SessionResolution{}, err
		}
		var envelope *sessionenv.Envelope
		if includeValues {
			envelope, err = sessionenv.NewEnvelope(nil)
			if err != nil {
				return SessionResolution{}, err
			}
		}
		return SessionResolution{Environment: envelope, ContentHash: contentHash}, nil
	}
	if len(selections) > sessionenv.MaxItems {
		return SessionResolution{}, ValidationError(fmt.Sprintf("a session supports at most %d Vault items", sessionenv.MaxItems))
	}
	tx, commit, rollback, err := s.transaction(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return SessionResolution{}, fmt.Errorf("begin Vault session resolution: %w", err)
	}
	defer rollback()

	items := make([]SessionItem, 0, len(selections))
	inputs := make([]sessionenv.EntryInput, 0, len(selections))
	defer func() {
		for index := range inputs {
			clear(inputs[index].Value)
			inputs[index].Value = nil
		}
	}()
	seenIDs := map[int64]bool{}
	for _, selection := range selections {
		if selection.ItemID < 1 || selection.SourceProjectID < 1 {
			return SessionResolution{}, ValidationError("item_id and source_project_id are required")
		}
		if seenIDs[selection.ItemID] {
			return SessionResolution{}, ValidationError("the same Vault item cannot be selected twice")
		}
		seenIDs[selection.ItemID] = true
		item, encrypted, encryptionVersion, expiresAt, err := resolveSessionItem(ctx, tx, selection)
		if err != nil {
			return SessionResolution{}, err
		}
		if expiresAt != "" {
			expiry, err := time.Parse(time.RFC3339, expiresAt)
			if err != nil || !expiry.After(time.Now().UTC()) {
				return SessionResolution{}, ValidationError(fmt.Sprintf("Vault item %s is expired", item.Name))
			}
		}
		items = append(items, item)
		if includeValues {
			aad, err := itemAssociatedData(s.workspaceUUID, item.ItemID, item.ValueVersion, encryptionVersion)
			if err != nil {
				return SessionResolution{}, err
			}
			var decoded encryptedItemValue
			if err := s.vault.DecryptJSONWithAAD(encrypted, &decoded, aad); err != nil {
				return SessionResolution{}, fmt.Errorf("decrypt Vault session item: %w", err)
			}
			inputs = append(inputs, sessionenv.EntryInput{
				Name: item.Name, Value: []byte(decoded.Value), ReplaceExisting: item.ReplaceExisting,
				ItemID: item.ItemID, ValueVersion: item.ValueVersion, SourceProjectID: item.SourceProjectID,
			})
			decoded.Value = ""
		}
	}
	var envelope *sessionenv.Envelope
	if includeValues {
		envelope, err = sessionenv.NewEnvelope(inputs)
		if err != nil {
			return SessionResolution{}, err
		}
	}
	contentHash, err := sessionContentHash(items)
	if err != nil {
		if envelope != nil {
			envelope.Destroy()
		}
		return SessionResolution{}, err
	}
	if err := commit(); err != nil {
		if envelope != nil {
			envelope.Destroy()
		}
		return SessionResolution{}, fmt.Errorf("commit Vault session resolution: %w", err)
	}
	return SessionResolution{Items: items, Environment: envelope, ContentHash: contentHash}, nil
}

func (s *Store) RevalidateSession(ctx context.Context, expected []SessionItem) error {
	selections := make([]SessionSelection, 0, len(expected))
	for _, item := range expected {
		selections = append(selections, SessionSelection{
			ItemID: item.ItemID, SourceProjectID: item.SourceProjectID,
			ReplaceExisting: item.ReplaceExisting, BindingID: item.BindingID,
			BindingRevision: item.BindingRevision,
		})
	}
	resolved, err := s.SnapshotSession(ctx, selections)
	if err != nil {
		return err
	}
	defer resolved.Destroy()
	expectedHash, err := sessionContentHash(expected)
	if err != nil {
		return err
	}
	if resolved.ContentHash != expectedHash {
		return ErrStale
	}
	return nil
}

func (s *Store) MarkSessionItemsUsed(ctx context.Context, items []SessionItem) error {
	if len(items) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, commit, rollback, err := s.transaction(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Vault usage update: %w", err)
	}
	defer rollback()
	for _, item := range items {
		result, err := tx.ExecContext(ctx, `
			UPDATE vault_items
			SET last_used_at = ?, usage_count = usage_count + 1
			WHERE id = ? AND status = 'active' AND value_version = ?`,
			now, item.ItemID, item.ValueVersion,
		)
		if err != nil {
			return fmt.Errorf("update Vault item usage: %w", err)
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return ErrStale
		}
	}
	if err := commit(); err != nil {
		return fmt.Errorf("commit Vault usage update: %w", err)
	}
	return nil
}

func resolveSessionItem(ctx context.Context, tx storeDB, selection SessionSelection) (SessionItem, string, int, string, error) {
	var item SessionItem
	var encrypted, expiresAt, status string
	var encryptionVersion, sourceActive, assigned int
	err := tx.QueryRowContext(ctx, `
		SELECT vi.id, vi.name, vi.value_version, vi.metadata_revision, vi.encrypted_value,
		       vi.encryption_version, COALESCE(vi.expires_at, ''), vi.status,
		       EXISTS(SELECT 1 FROM projects p WHERE p.id = ? AND p.status = 'active'),
		       (
		         vi.owner_project_id = ?
		         OR EXISTS(
		           SELECT 1 FROM vault_item_projects vip
		           WHERE vip.vault_item_id = vi.id AND vip.project_id = ?
		         )
		       )
		FROM vault_items vi WHERE vi.id = ?`,
		selection.SourceProjectID, selection.SourceProjectID, selection.SourceProjectID, selection.ItemID,
	).Scan(
		&item.ItemID, &item.Name, &item.ValueVersion, &item.MetadataRevision,
		&encrypted, &encryptionVersion, &expiresAt, &status, &sourceActive, &assigned,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionItem{}, "", 0, "", ErrNotFound
	}
	if err != nil {
		return SessionItem{}, "", 0, "", fmt.Errorf("resolve Vault session item: %w", err)
	}
	if status != "active" || sourceActive == 0 || assigned == 0 {
		return SessionItem{}, "", 0, "", ValidationError("Vault item is not active for the source project")
	}
	item.SourceProjectID = selection.SourceProjectID
	item.ReplaceExisting = selection.ReplaceExisting
	item.BindingID = selection.BindingID
	item.BindingRevision = selection.BindingRevision
	if selection.BindingID > 0 {
		var actualRevision int64
		err := tx.QueryRowContext(ctx, `
			SELECT binding_revision FROM vault_default_bindings
			WHERE id = ? AND vault_item_id = ? AND source_project_id = ?`,
			selection.BindingID, selection.ItemID, selection.SourceProjectID,
		).Scan(&actualRevision)
		if errors.Is(err, sql.ErrNoRows) {
			return SessionItem{}, "", 0, "", ErrStale
		}
		if err != nil {
			return SessionItem{}, "", 0, "", fmt.Errorf("validate Vault binding revision: %w", err)
		}
		if actualRevision != selection.BindingRevision {
			return SessionItem{}, "", 0, "", ErrStale
		}
	}
	return item, encrypted, encryptionVersion, expiresAt, nil
}

func sessionContentHash(items []SessionItem) (string, error) {
	ordered := append([]SessionItem(nil), items...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Name == ordered[j].Name {
			return ordered[i].ItemID < ordered[j].ItemID
		}
		return ordered[i].Name < ordered[j].Name
	})
	payload, err := json.Marshal(struct {
		Schema string        `json:"schema"`
		Items  []SessionItem `json:"items"`
	}{Schema: "vault-session-environment-v1", Items: ordered})
	if err != nil {
		return "", fmt.Errorf("encode Vault session content hash: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func ValidateSessionItemName(name string) error {
	return sessionenv.ValidateName(strings.TrimSpace(name))
}
