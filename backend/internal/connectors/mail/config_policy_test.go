package mailconnector

import (
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestMailPolicyRejectsMalformedListsBeforeDispatch(t *testing.T) {
	for _, value := range []any{true, 42, map[string]any{"allowed": "example.com"}, []any{nil}, []any{true}, []any{"example.com", 42}, []string{" "}} {
		target := connectors.TargetView{Config: validTargetConfig()}
		target.Config["allowed_recipient_domains"] = value
		profile := connectors.CredentialProfileView{Public: validProfilePublic()}
		assertInvalidMailPolicy(t, target, profile, func() error { return Connector{}.ValidateTargetConfig(target.Config) })
		for _, field := range []string{"allowed_read_folders", "allowed_mutation_source_folders", "allowed_mutation_destination_folders"} {
			target = connectors.TargetView{Config: validTargetConfig()}
			profile = connectors.CredentialProfileView{Public: validProfilePublic()}
			profile.Public[field] = value
			assertInvalidMailPolicy(t, target, profile, func() error {
				return Connector{}.ValidateCredentialProfile("password", profile.Public, map[string]any{"imap_username": "fixture", "imap_password": "fixture-only"}, nil)
			})
		}
	}
}

func assertInvalidMailPolicy(t *testing.T, target connectors.TargetView, profile connectors.CredentialProfileView, validate func() error) {
	t.Helper()
	if err := validate(); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("malformed policy admitted: target=%#v profile=%#v err=%v", target.Config, profile.Public, err)
	}
	_, err := Connector{}.PrepareAction(t.Context(), connectors.ActionRequest{Target: target, Profile: profile, ActionName: ActionListFolders})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("malformed policy admitted at preparation: %v", err)
	}
	_, err = Connector{}.ExecuteAction(t.Context(), connectors.RuntimeContext{Target: target, Profile: profile}, connectors.PreparedAction{ActionName: ActionListFolders})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("malformed policy reached runtime transport: %v", err)
	}
}

func TestMailPolicyPreservesValidListsAndNilDefaults(t *testing.T) {
	for _, value := range []any{[]string{" example.com ", "example.com"}, []any{"example.com"}, "example.com\nexample.com"} {
		target := validTargetConfig()
		target["allowed_recipient_domains"] = value
		config, err := targetConfigFrom(connectors.TargetView{Config: target})
		if err != nil || len(config.AllowedRecipientDomains) != 1 || config.AllowedRecipientDomains[0] != "example.com" {
			t.Fatalf("valid policy changed: %#v err=%v", config, err)
		}
		if _, err := parseAddressInput([]string{"reader@outside.test"}, config.AllowedRecipientDomains); err == nil {
			t.Fatal("recipient outside the policy admitted")
		}
	}
	for _, value := range []any{nil, []string{}, []any{}, ""} {
		target := validTargetConfig()
		target["allowed_recipient_domains"] = value
		if config, err := targetConfigFrom(connectors.TargetView{Config: target}); err != nil || len(config.AllowedRecipientDomains) != 0 {
			t.Fatalf("explicit unrestricted/default policy rejected: %#v err=%v", config, err)
		}
	}
	public := validProfilePublic()
	delete(public, "allowed_read_folders")
	delete(public, "allowed_mutation_source_folders")
	delete(public, "allowed_mutation_destination_folders")
	delete(public, "archive_folder")
	delete(public, "trash_folder")
	config, err := profileConfigFrom(connectors.CredentialProfileView{Public: public})
	if err != nil || len(config.AllowedReadFolders) != 1 || config.AllowedReadFolders[0] != "INBOX" || len(config.AllowedMutationSources) != 1 || len(config.AllowedMutationDestinations) != 0 {
		t.Fatalf("nil folder defaults changed: %#v err=%v", config, err)
	}
}
