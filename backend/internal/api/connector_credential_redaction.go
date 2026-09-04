package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

const (
	connectorCredentialRedactionMarker   = "[REDACTED CREDENTIAL]"
	connectorCredentialSubstringMinBytes = 8
	connectorCredentialDelimitedMinBytes = 1
)

// connectorCredentialBoundary is an unconditional last-mile boundary for
// values the gateway makes available to connector code. Operator-configured
// redaction may be disabled; delivered credentials must never be returned or
// persisted regardless of that setting.
type connectorCredentialBoundary struct {
	state *connectorCredentialBoundaryState
}

type connectorCredentialBoundaryState struct {
	mu     sync.RWMutex
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
	return connectorCredentialBoundary{state: &connectorCredentialBoundaryState{values: deduplicateSortedStrings(values)}}
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

func connectorActionSensitiveValues(input map[string]any, payload map[string]any, sensitiveFields []string) []string {
	fields := make(map[string]bool, len(sensitiveFields))
	for _, field := range sensitiveFields {
		if normalized := normalizeConnectorOutputField(field); normalized != "" {
			fields[normalized] = true
		}
	}
	if len(fields) == 0 {
		return nil
	}
	values := map[string]struct{}{}
	collectDeclaredSensitiveValues(input, fields, false, values)
	collectDeclaredSensitiveValues(payload, fields, false, values)
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i]) == len(result[j]) {
			return result[i] < result[j]
		}
		return len(result[i]) > len(result[j])
	})
	return result
}

func redactConnectorActionSensitiveText(value string, sensitiveValues []string) string {
	sort.Slice(sensitiveValues, func(i, j int) bool { return len(sensitiveValues[i]) > len(sensitiveValues[j]) })
	for _, sensitive := range sensitiveValues {
		if sensitive == "" {
			continue
		}
		if value == sensitive || len(sensitive) >= connectorCredentialSubstringMinBytes {
			value = strings.ReplaceAll(value, sensitive, connectorCredentialRedactionMarker)
		} else {
			value = redactDelimitedCredential(value, sensitive)
		}
	}
	return value
}

func collectDeclaredSensitiveValues(value any, fields map[string]bool, sensitive bool, values map[string]struct{}) {
	if sensitive {
		collectSensitiveValueStrings(value, values)
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			collectDeclaredSensitiveValues(item, fields, fields[normalizeConnectorOutputField(key)], values)
		}
	case []any:
		for _, item := range typed {
			collectDeclaredSensitiveValues(item, fields, false, values)
		}
	}
}

func collectSensitiveValueStrings(value any, values map[string]struct{}) {
	if encoded, err := json.Marshal(value); err == nil && len(encoded) > 0 && string(encoded) != "null" {
		values[string(encoded)] = struct{}{}
	}
	switch typed := value.(type) {
	case string:
		if typed != "" {
			values[typed] = struct{}{}
		}
	case map[string]any:
		for _, item := range typed {
			collectSensitiveValueStrings(item, values)
		}
	case []any:
		for _, item := range typed {
			collectSensitiveValueStrings(item, values)
		}
	case nil:
	default:
		values[fmt.Sprint(typed)] = struct{}{}
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
		strings.TrimSpace(secret),
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
	if r.state == nil {
		return value
	}
	r.state.mu.RLock()
	values := append([]string(nil), r.state.values...)
	r.state.mu.RUnlock()
	for _, secret := range values {
		if value == secret {
			return connectorCredentialRedactionMarker
		}
		if len(secret) >= connectorCredentialSubstringMinBytes {
			value = strings.ReplaceAll(value, secret, connectorCredentialRedactionMarker)
		} else if len(secret) >= connectorCredentialDelimitedMinBytes {
			value = redactDelimitedCredential(value, secret)
		}
	}
	return value
}

func (r connectorCredentialBoundary) RedactKey(value string) string {
	if r.state == nil {
		return value
	}
	r.state.mu.RLock()
	values := append([]string(nil), r.state.values...)
	r.state.mu.RUnlock()
	for _, secret := range values {
		if value == secret {
			return connectorCredentialRedactionMarker
		}
		if len(secret) >= connectorCredentialSubstringMinBytes {
			value = strings.ReplaceAll(value, secret, connectorCredentialRedactionMarker)
		} else if len(secret) >= connectorCredentialDelimitedMinBytes {
			value = redactDelimitedCredential(value, secret)
		}
	}
	return value
}

func redactDelimitedCredential(value string, secret string) string {
	start := 0
	for {
		index := strings.Index(value[start:], secret)
		if index < 0 {
			return value
		}
		index += start
		end := index + len(secret)
		leftDelimited := index == 0 || !credentialWordByte(value[index-1])
		rightDelimited := end == len(value) || !credentialWordByte(value[end])
		if leftDelimited && rightDelimited {
			value = value[:index] + connectorCredentialRedactionMarker + value[end:]
			start = index + len(connectorCredentialRedactionMarker)
			continue
		}
		start = end
	}
}

func credentialWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_'
}

func (r connectorCredentialBoundary) Empty() bool {
	if r.state == nil {
		return true
	}
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	return len(r.state.values) == 0
}

func (r connectorCredentialBoundary) Add(values ...string) {
	if r.state == nil {
		return
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	combined := append([]string(nil), r.state.values...)
	for _, value := range values {
		combined = append(combined, connectorCredentialVariants(value)...)
	}
	sort.Slice(combined, func(i, j int) bool {
		if len(combined[i]) == len(combined[j]) {
			return combined[i] < combined[j]
		}
		return len(combined[i]) > len(combined[j])
	})
	r.state.values = deduplicateSortedStrings(combined)
}

func (r connectorCredentialBoundary) AddStructured(value any) {
	if r.state == nil {
		return
	}
	unique := map[string]struct{}{}
	collectConnectorCredentialStrings(value, unique)
	values := make([]string, 0, len(unique))
	for value := range unique {
		values = append(values, value)
	}
	r.Add(values...)
}

func (r connectorCredentialBoundary) RedactStructured(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[r.RedactKey(key)] = r.RedactStructured(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = r.RedactStructured(item)
		}
		return result
	case string:
		return r.Redact(typed)
	default:
		return value
	}
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
