package api

import (
	"context"
	"encoding/base64"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

const connectorCredentialRedactionMarker = "[REDACTED CREDENTIAL]"

// connectorCredentialBoundary is an unconditional last-mile boundary for
// values the gateway makes available to connector code. Operator-configured
// redaction may be disabled; delivered credentials must never be returned or
// persisted regardless of that setting.
type connectorCredentialBoundary struct {
	values []string
}

func newConnectorCredentialBoundary(secrets map[string]any) connectorCredentialBoundary {
	unique := map[string]struct{}{}
	collectConnectorCredentialStrings(secrets, unique)
	values := make([]string, 0, len(unique)*4)
	for secret := range unique {
		for _, variant := range connectorCredentialVariants(secret) {
			if variant != "" && variant != connectorCredentialRedactionMarker {
				values = append(values, variant)
			}
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if len(values[i]) == len(values[j]) {
			return values[i] < values[j]
		}
		return len(values[i]) > len(values[j])
	})
	return connectorCredentialBoundary{values: deduplicateSortedStrings(values)}
}

func combinedConnectorCredentialBoundary(secretSets ...map[string]any) connectorCredentialBoundary {
	combined := make(map[string]any, len(secretSets))
	for index, secrets := range secretSets {
		combined[strconv.Itoa(index)] = secrets
	}
	return newConnectorCredentialBoundary(combined)
}

func collectConnectorCredentialStrings(value any, values map[string]struct{}) {
	switch typed := value.(type) {
	case string:
		if typed != "" {
			values[typed] = struct{}{}
		}
	case map[string]any:
		for _, item := range typed {
			collectConnectorCredentialStrings(item, values)
		}
	case []any:
		for _, item := range typed {
			collectConnectorCredentialStrings(item, values)
		}
	}
}

func connectorCredentialVariants(secret string) []string {
	if secret == "" {
		return nil
	}
	quoted := strconv.Quote(secret)
	if len(quoted) >= 2 {
		quoted = quoted[1 : len(quoted)-1]
	}
	encoded := []string{
		secret,
		quoted,
		url.QueryEscape(secret),
		url.PathEscape(secret),
		base64.StdEncoding.EncodeToString([]byte(secret)),
		base64.RawStdEncoding.EncodeToString([]byte(secret)),
		base64.URLEncoding.EncodeToString([]byte(secret)),
		base64.RawURLEncoding.EncodeToString([]byte(secret)),
	}
	return deduplicateSortedStrings(encoded)
}

func deduplicateSortedStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (r connectorCredentialBoundary) Redact(value string) string {
	for _, secret := range r.values {
		value = strings.ReplaceAll(value, secret, connectorCredentialRedactionMarker)
	}
	return value
}

func (r connectorCredentialBoundary) Empty() bool {
	return len(r.values) == 0
}

func connectorCredentialBoundaryForRuntimeID(ctx context.Context, runtime *databaseRuntime, runtimeID int64) (connectorCredentialBoundary, error) {
	if runtime == nil || runtime.database == nil || runtime.vault == nil {
		return connectorCredentialBoundary{}, nil
	}
	store := connectortargets.NewStore(runtime.database)
	_, profileView, _, err := store.TargetProfileByRuntimeID(ctx, runtimeID)
	if err != nil {
		return connectorCredentialBoundary{}, err
	}
	profile, err := store.GetCredentialProfile(ctx, profileView.TargetID, profileView.ID)
	if err != nil {
		return connectorCredentialBoundary{}, err
	}
	if profile.EncryptedSecretJSON == "" {
		return connectorCredentialBoundary{}, nil
	}
	secrets := map[string]any{}
	if err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profile.ID, profile.EncryptedSecretJSON, &secrets); err != nil {
		return connectorCredentialBoundary{}, err
	}
	return newConnectorCredentialBoundary(secrets), nil
}
