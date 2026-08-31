package api

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func stringConfigValue(config map[string]any, key string) string {
	value, ok := config[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(toString(value))
}

func intConfigValue(config map[string]any, key string, fallback int) int {
	value, ok := config[key]
	if !ok || value == nil {
		return fallback
	}
	parsed, ok := configIntValue(value)
	if !ok || parsed == 0 {
		return fallback
	}
	return parsed
}

func configIntValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		if typed < int64(math.MinInt) || typed > int64(math.MaxInt) {
			return 0, false
		}
		return int(typed), true
	case float64:
		if typed < float64(math.MinInt) || typed > float64(math.MaxInt) {
			return 0, false
		}
		return int(typed), true
	case json.Number:
		parsed, err := strconv.ParseInt(string(typed), 10, strconv.IntSize)
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		parsed, err := strconv.ParseInt(strings.TrimSpace(toString(value)), 10, strconv.IntSize)
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	}
}

func int64ConfigValue(config map[string]any, key string) int64 {
	value, ok := config[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := strconv.ParseInt(string(typed), 10, 64)
		return parsed
	default:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(toString(value)), 10, 64)
		return parsed
	}
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}

func connectorTargetToResponse(target connectortargets.Target, profiles []connectortargets.CredentialProfile) connectorTargetResponse {
	return connectorTargetResponse{
		ID:            target.ID,
		ProjectID:     target.ProjectID,
		ProjectName:   target.ProjectName,
		ProjectSlug:   target.ProjectSlug,
		ConnectorKind: target.ConnectorKind,
		Name:          target.Name,
		Config:        target.Config,
		Status:        string(target.Status),
		CreatedAt:     target.CreatedAt,
		UpdatedAt:     target.UpdatedAt,
		Profiles:      profileSummaries(profiles),
	}
}

func profileSummaries(profiles []connectortargets.CredentialProfile) []profileSummary {
	if profiles == nil {
		return nil
	}
	items := make([]profileSummary, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, profileToSummary(profile))
	}
	return items
}

func profileToSummary(profile connectortargets.CredentialProfile) profileSummary {
	return profileSummary{
		ID:            profile.ID,
		TargetID:      profile.TargetID,
		Ref:           connectortargets.ConnectorTargetRef(profile.ConnectorKind, profile.TargetID, profile.ID),
		ConnectorKind: profile.ConnectorKind,
		Kind:          profile.Kind,
		Label:         profile.Label,
		Public:        profile.Public,
		RiskLabel:     profile.RiskLabel,
		CreatedAt:     profile.CreatedAt,
		UpdatedAt:     profile.UpdatedAt,
	}
}
