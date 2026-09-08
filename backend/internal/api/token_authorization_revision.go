package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/projectcapabilities"
	"github.com/aipermission/aipermission/backend/internal/projects"
)

var (
	errAuthorizationRevisionRequired = errors.New("authorization revision is required")
	errAuthorizationRevisionConflict = errors.New("authorization changed since it was loaded")
)

func authorizationRevision(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func connectorPermissionsRevision(items []connectortargets.ActionPermission) (string, error) {
	type revisionItem struct {
		TargetID      int64  `json:"target_id"`
		ProfileID     int64  `json:"profile_id"`
		ActionName    string `json:"action_name"`
		ExecutionRule string `json:"execution_rule"`
		ExpiresAt     string `json:"expires_at"`
	}
	values := make([]revisionItem, 0, len(items))
	for _, item := range items {
		values = append(values, revisionItem{
			TargetID: item.TargetID, ProfileID: item.ProfileID, ActionName: item.ActionName,
			ExecutionRule: string(item.ExecutionRule), ExpiresAt: item.ExpiresAt,
		})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].TargetID != values[j].TargetID {
			return values[i].TargetID < values[j].TargetID
		}
		if values[i].ProfileID != values[j].ProfileID {
			return values[i].ProfileID < values[j].ProfileID
		}
		return values[i].ActionName < values[j].ActionName
	})
	return authorizationRevision(values)
}

func projectScopesRevision(items []projects.TokenScope) (string, error) {
	type revisionItem struct {
		ProjectID int64 `json:"project_id"`
		Enabled   bool  `json:"enabled"`
	}
	values := make([]revisionItem, 0, len(items))
	for _, item := range items {
		values = append(values, revisionItem{ProjectID: item.ProjectID, Enabled: item.Enabled})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ProjectID < values[j].ProjectID })
	return authorizationRevision(values)
}

func projectCapabilitiesRevision(items []projectcapabilities.Capability) (string, error) {
	type revisionItem struct {
		ProjectID      int64  `json:"project_id"`
		Name           string `json:"capability_name"`
		ExecutionRule  string `json:"execution_rule"`
		ExpiresAt      string `json:"expires_at"`
		RecordRevision int64  `json:"record_revision"`
	}
	values := make([]revisionItem, 0, len(items))
	for _, item := range items {
		values = append(values, revisionItem{
			ProjectID: item.ProjectID, Name: item.Name, ExecutionRule: item.ExecutionRule,
			ExpiresAt: item.ExpiresAt, RecordRevision: item.Revision,
		})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].ProjectID != values[j].ProjectID {
			return values[i].ProjectID < values[j].ProjectID
		}
		return values[i].Name < values[j].Name
	})
	return authorizationRevision(values)
}

func requireAuthorizationRevision(expected, current string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(expected) == "" {
		return current, errAuthorizationRevisionRequired
	}
	if !strings.EqualFold(strings.TrimSpace(expected), current) {
		return current, errAuthorizationRevisionConflict
	}
	return current, nil
}

func handleAuthorizationRevisionError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, errAuthorizationRevisionRequired):
		writeError(w, http.StatusBadRequest, "authorization revision is required; reload permissions and retry")
		return true
	case errors.Is(err, errAuthorizationRevisionConflict):
		writeError(w, http.StatusConflict, "authorization changed in another client; reload permissions and retry")
		return true
	default:
		return false
	}
}
