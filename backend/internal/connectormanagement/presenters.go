package connectormanagement

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TargetToResponse(target connectortargets.Target, profiles []connectortargets.CredentialProfile) TargetResponse {
	return TargetResponse{
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
		Profiles:      ProfileSummaries(profiles),
	}
}

func ProfileSummaries(profiles []connectortargets.CredentialProfile) []ProfileSummary {
	if profiles == nil {
		return nil
	}
	items := make([]ProfileSummary, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, ProfileToSummary(profile))
	}
	return items
}

func ProfileToSummary(profile connectortargets.CredentialProfile) ProfileSummary {
	return ProfileSummary{
		ID:            profile.ID,
		TargetID:      profile.TargetID,
		Ref:           connectors.FormatTargetRef(profile.ConnectorKind, profile.TargetID, profile.ID),
		ConnectorKind: profile.ConnectorKind,
		Kind:          profile.Kind,
		Label:         profile.Label,
		Public:        profile.Public,
		RiskLabel:     profile.RiskLabel,
		CreatedAt:     profile.CreatedAt,
		UpdatedAt:     profile.UpdatedAt,
	}
}
