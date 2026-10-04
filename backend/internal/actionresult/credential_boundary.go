package actionresult

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	CredentialRedactionMarker            = "[REDACTED CREDENTIAL]"
	connectorCredentialSubstringMinBytes = 3
	connectorCredentialKeySubstringBytes = 8
)

// NormalizeField converts connector field names to the canonical form used by
// output hints and persisted action projections.
func NormalizeField(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.ReplaceAll(value, "-", "_")
}

// CredentialBoundary is an unconditional last-mile boundary for
// values the gateway makes available to connector code. Operator-configured
// redaction may be disabled; delivered credentials must never be returned or
// persisted regardless of that setting.
type CredentialBoundary struct {
	state *credentialBoundaryState
}

type credentialBoundaryState struct {
	mu     sync.RWMutex
	values []string
}

// Valid reports whether the boundary was initialized. An initialized boundary
// may be empty when a credential profile contains no secret values.
func (r CredentialBoundary) Valid() bool {
	return r.state != nil
}

func NewCredentialBoundary(secrets map[string]any) CredentialBoundary {
	unique := map[string]struct{}{}
	collectConnectorCredentialStrings(secrets, unique)
	values := make([]string, 0, len(unique)*4)
	for secret := range unique {
		for _, variant := range connectorCredentialVariants(secret) {
			if variant != "" && variant != CredentialRedactionMarker {
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
	return CredentialBoundary{state: &credentialBoundaryState{values: deduplicateSortedStrings(values)}}
}

func CombinedCredentialBoundary(secretSets ...map[string]any) CredentialBoundary {
	combined := make(map[string]any, len(secretSets))
	for index, secrets := range secretSets {
		combined[strconv.Itoa(index)] = secrets
	}
	return NewCredentialBoundary(combined)
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
	case json.Number, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		collectSensitiveValueStrings(typed, values)
	}
}

func SensitiveValues(input map[string]any, payload map[string]any, sensitiveFields []string) []string {
	fields := make(map[string]bool, len(sensitiveFields))
	for _, field := range sensitiveFields {
		if normalized := NormalizeField(field); normalized != "" {
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

func RedactSensitiveText(value string, sensitiveValues []string) string {
	sort.Slice(sensitiveValues, func(i, j int) bool { return len(sensitiveValues[i]) > len(sensitiveValues[j]) })
	for _, sensitive := range sensitiveValues {
		if sensitive == "" {
			continue
		}
		value = redactCredentialValue(value, sensitive, connectorCredentialSubstringMinBytes)
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
			collectDeclaredSensitiveValues(item, fields, fields[NormalizeField(key)], values)
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
	jsonQuoted := ""
	if encoded, err := json.Marshal(secret); err == nil && len(encoded) >= 2 {
		jsonQuoted = string(encoded[1 : len(encoded)-1])
	}
	encoded := []string{
		secret,
		strings.TrimSpace(secret),
		quoted,
		jsonQuoted,
		html.EscapeString(secret),
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

func (r CredentialBoundary) Redact(value string) string {
	if r.state == nil {
		return value
	}
	r.state.mu.RLock()
	values := append([]string(nil), r.state.values...)
	r.state.mu.RUnlock()
	for _, secret := range values {
		value = redactCredentialValue(value, secret, connectorCredentialSubstringMinBytes)
	}
	return value
}

func (r CredentialBoundary) RedactKey(value string) string {
	if r.state == nil {
		return value
	}
	r.state.mu.RLock()
	values := append([]string(nil), r.state.values...)
	r.state.mu.RUnlock()
	for _, secret := range values {
		value = redactCredentialValue(value, secret, connectorCredentialKeySubstringBytes)
	}
	return value
}

func redactCredentialValue(value, secret string, substringMinBytes int) string {
	if secret == "" || !strings.Contains(value, secret) {
		return value
	}
	var output strings.Builder
	start := 0
	for {
		index := strings.Index(value[start:], secret)
		if index < 0 {
			output.WriteString(value[start:])
			return output.String()
		}
		index += start
		end := index + len(secret)
		leftDelimited := index == 0 || !credentialWordByte(value[index-1])
		rightDelimited := end == len(value) || !credentialWordByte(value[end])
		mask := value == secret || len(secret) >= substringMinBytes || leftDelimited && rightDelimited
		output.WriteString(value[start:index])
		if mask && !credentialMatchInsideMarker(value, index, end) {
			output.WriteString(CredentialRedactionMarker)
		} else {
			output.WriteString(secret)
		}
		start = end
	}
}

func credentialMatchInsideMarker(value string, start, end int) bool {
	// Only matches wholly inside an existing marker are protected. Searching
	// the full value still finds credentials that span either marker edge.
	windowStart := max(0, start-len(CredentialRedactionMarker)+1)
	windowEnd := min(len(value), start+len(CredentialRedactionMarker))
	index := strings.Index(value[windowStart:windowEnd], CredentialRedactionMarker)
	if index < 0 {
		return false
	}
	markerStart := windowStart + index
	return markerStart <= start && end <= markerStart+len(CredentialRedactionMarker)
}

func credentialWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_'
}

func (r CredentialBoundary) Empty() bool {
	if r.state == nil {
		return true
	}
	r.state.mu.RLock()
	defer r.state.mu.RUnlock()
	return len(r.state.values) == 0
}

func (r CredentialBoundary) Add(values ...string) {
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

func (r CredentialBoundary) AddStructured(value any) {
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

func (r CredentialBoundary) RedactStructured(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			redactedKey := r.RedactKey(key)
			if _, exists := result[redactedKey]; exists {
				return nil, fmt.Errorf("%w: credential redaction produced duplicate object key", ErrInvalidValue)
			}
			redactedItem, err := r.RedactStructured(item)
			if err != nil {
				return nil, err
			}
			result[redactedKey] = redactedItem
		}
		return result, nil
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			redactedItem, err := r.RedactStructured(item)
			if err != nil {
				return nil, err
			}
			result[index] = redactedItem
		}
		return result, nil
	case string:
		return r.Redact(typed), nil
	case json.Number, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return redactScalar(typed, r.Redact), nil
	default:
		return value, nil
	}
}
