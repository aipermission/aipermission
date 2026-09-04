package api

import (
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

type concurrentCredentialTestConnector struct {
	localActionTestConnector
	validationCalls atomic.Int32
	firstEntered    chan struct{}
	releaseFirst    chan struct{}
}

func (*concurrentCredentialTestConnector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{
		Kind: "default", Label: "Default",
		Schema: connectors.Schema{Fields: []connectors.Field{
			{Name: "first_secret", Label: "First secret", Type: connectors.FieldSecret, Required: true, Secret: true},
			{Name: "second_secret", Label: "Second secret", Type: connectors.FieldSecret, Required: true, Secret: true},
		}},
	}}
}

func (connector *concurrentCredentialTestConnector) ValidateCredentialProfile(string, map[string]any, map[string]any, *connectors.CredentialProfileView) error {
	if connector.validationCalls.Add(1) == 1 {
		close(connector.firstEntered)
		<-connector.releaseFirst
	}
	return nil
}

func TestConcurrentPartialCredentialUpdatesPreserveBothChanges(t *testing.T) {
	fixture := newAPITestFixture(t)
	connector := &concurrentCredentialTestConnector{
		firstEntered: make(chan struct{}), releaseFirst: make(chan struct{}),
	}
	if err := fixture.server.activeRuntime().connectorRegistry().Register(connector); err != nil {
		t.Fatalf("register concurrency connector: %v", err)
	}
	store := connectortargets.NewStore(fixture.db)
	target, err := store.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: localActionTestConnectorKind, Name: "concurrent-credential", Config: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: localActionTestConnectorKind, Kind: "default", Label: "main", Public: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := recordcrypto.EncryptJSON(
		fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID,
		recordcrypto.ConnectorCredentialProfile, profile.ID,
		map[string]any{"first_secret": "old-first", "second_secret": "old-second"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetCredentialProfileEncryptedSecret(t.Context(), target.ID, profile.ID, encrypted); err != nil {
		t.Fatal(err)
	}

	path := "/api/connector-targets/" + strconv.FormatInt(target.ID, 10) + "/profiles/" + strconv.FormatInt(profile.ID, 10)
	type response struct {
		status int
		body   string
	}
	firstDone := make(chan response, 1)
	go func() {
		result := performJSON(fixture.server.Handler(), http.MethodPut, path, "", updateConnectorCredentialProfileRequest{
			Kind: "default", Label: "main", Public: map[string]any{}, Secret: map[string]any{"first_secret": "new-first"},
		})
		firstDone <- response{status: result.Code, body: result.Body.String()}
	}()
	select {
	case <-connector.firstEntered:
	case <-time.After(5 * time.Second):
		close(connector.releaseFirst)
		t.Fatal("first credential update did not reach validation")
	}
	secondDone := make(chan response, 1)
	go func() {
		result := performJSON(fixture.server.Handler(), http.MethodPut, path, "", updateConnectorCredentialProfileRequest{
			Kind: "default", Label: "main", Public: map[string]any{}, Secret: map[string]any{"second_secret": "new-second"},
		})
		secondDone <- response{status: result.Code, body: result.Body.String()}
	}()
	close(connector.releaseFirst)
	for _, result := range []response{<-firstDone, <-secondDone} {
		if result.status != http.StatusOK {
			t.Fatalf("concurrent credential update failed: %d %s", result.status, result.body)
		}
	}

	stored, err := store.GetCredentialProfile(t.Context(), target.ID, profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	var secret map[string]any
	if err := recordcrypto.DecryptJSON(
		fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID,
		recordcrypto.ConnectorCredentialProfile, profile.ID, stored.EncryptedSecretJSON, &secret,
	); err != nil {
		t.Fatal(err)
	}
	if secret["first_secret"] != "new-first" || secret["second_secret"] != "new-second" {
		t.Fatalf("concurrent partial updates lost data: %#v", secret)
	}
}
