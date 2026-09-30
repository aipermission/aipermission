package management

import (
	"cmp"
	"context"
	"slices"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/keycleanup"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func cleanupReconciliationSnapshot(ctx context.Context, gateway connectorapi.PeerIdentityGateway, runtime connectorapi.ConnectorDataRuntime, target connectorapi.Target) (cleanupReconciliationContext, error) {
	profiles, err := runtime.ListCredentialProfiles(ctx, target.ID)
	if err != nil {
		return cleanupReconciliationContext{}, err
	}
	groups := []keyCleanupGroup{}
	if len(profiles) > 0 {
		groups, err = planKeyCleanup(ctx, gateway, runtime, target, profiles)
		if err != nil {
			return cleanupReconciliationContext{}, err
		}
	}
	profiles = slices.Clone(profiles)
	slices.SortFunc(profiles, func(a, b connectors.CredentialProfileView) int { return cmp.Compare(a.ID, b.ID) })
	digest, err := keycleanup.Digest(struct {
		TargetID  int64
		ProjectID int64
		Name      string
		Revision  string
		Config    map[string]any
		Profiles  []connectors.CredentialProfileView
		Groups    []keyCleanupGroup
	}{TargetID: target.ID, ProjectID: target.ProjectID, Name: target.Name, Revision: target.UpdatedAt, Config: target.Config, Profiles: profiles, Groups: groups})
	if err != nil {
		return cleanupReconciliationContext{}, err
	}
	identities := make([]keycleanup.Identity, len(groups))
	for index, group := range groups {
		identities[index] = group.Identity
	}
	entries, err := keycleanup.New(runtime.CredentialResources(keycleanup.ResourceKind)).ListForTarget(ctx, target.ID, identities...)
	if err != nil {
		return cleanupReconciliationContext{}, err
	}
	result := cleanupReconciliationContext{TargetID: target.ID, ContextDigest: digest, Records: []cleanupRecordView{}}
	for _, entry := range entries {
		view, err := cleanupRecordChoices(entry, identities)
		if err != nil {
			return cleanupReconciliationContext{}, err
		}
		result.Records = append(result.Records, view)
	}
	slices.SortFunc(result.Records, func(a, b cleanupRecordView) int { return cmp.Compare(a.Entry.ResourceID, b.Entry.ResourceID) })
	return result, nil
}

func cleanupRecordChoices(entry keycleanup.Entry, identities []keycleanup.Identity) (cleanupRecordView, error) {
	selected := []keycleanup.Identity{}
	for _, identity := range identities {
		if entry.Record.MatchesIdentity(identity) {
			selected = append(selected, identity)
		}
	}
	if len(selected) == 0 {
		selected = append(selected, entry.Record.Identity)
	}
	view := cleanupRecordView{Entry: entry, Choices: []cleanupIdentityChoice{}}
	for _, identity := range selected {
		digest, err := keycleanup.Digest(identity)
		if err != nil {
			return cleanupRecordView{}, err
		}
		subjects, err := keycleanup.VerificationSubjects(entry.Record, identity)
		if err != nil {
			return cleanupRecordView{}, err
		}
		view.Choices = append(view.Choices, cleanupIdentityChoice{Identity: identity, Digest: digest, Subjects: subjects})
	}
	return view, nil
}
