//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/api"
	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const listenAddress = "127.0.0.1:18080"

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	harness, err := newHarness(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer harness.close()

	log.Printf("AIPermission real-browser test backend listening on %s", listenAddress)
	if err := http.ListenAndServe(listenAddress, harness); err != nil {
		log.Fatal(err)
	}
}

type harness struct {
	mu      sync.RWMutex
	config  config.Config
	server  *api.Server
	handler http.Handler
}

func newHarness(cfg config.Config) (*harness, error) {
	h := &harness{config: cfg}
	if err := h.restart(); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *harness) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/__e2e/ready":
		w.WriteHeader(http.StatusNoContent)
		return
	case "/__e2e/restart":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := h.restart(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	handler := h.handler
	if handler == nil {
		http.Error(w, "test backend is restarting", http.StatusServiceUnavailable)
		return
	}
	handler.ServeHTTP(w, r)
}

func (h *harness) restart() error {
	registry := connectors.NewRegistry()
	if err := registry.Register(testConnector{}); err != nil {
		return fmt.Errorf("register test connector: %w", err)
	}
	server := api.NewLockedServer(h.config, api.WithConnectorRegistry(registry))

	h.mu.Lock()
	defer h.mu.Unlock()
	previous := h.server
	h.server = server
	h.handler = server.Handler()
	if previous != nil {
		previous.Close()
	}
	return nil
}

func (h *harness) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.server != nil {
		h.server.Close()
		h.server = nil
		h.handler = nil
	}
}

type testConnector struct{}

func (testConnector) Kind() string    { return "e2e" }
func (testConnector) Label() string   { return "E2E" }
func (testConnector) Version() string { return "0.1" }
func (testConnector) TargetSchema() connectors.Schema {
	return connectors.Schema{}
}
func (testConnector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{Kind: "local", Label: "Local", Schema: connectors.Schema{}}}
}
func (testConnector) GetHelp(_ context.Context, target connectors.TargetView) (connectors.ConnectorHelp, error) {
	return connectors.ConnectorHelp{
		Title:       "Deterministic E2E connector",
		Summary:     "Exercises the real gateway lifecycle without an external service.",
		Connector:   "e2e",
		ConnectorID: target.Ref,
	}, nil
}
func (testConnector) GetActionList(context.Context, connectors.TargetView, connectors.CredentialProfileView) ([]connectors.ActionDefinition, error) {
	return []connectors.ActionDefinition{{
		Name:        "echo",
		Label:       "Echo",
		Description: "Return one deterministic message.",
		Risk:        connectors.RiskRead,
		InputSchema: connectors.Schema{Fields: []connectors.Field{{Name: "message", Label: "Message", Type: connectors.FieldString, Required: true}}},
		OutputHint:  connectors.OutputHint{Format: "json"},
	}}, nil
}
func (testConnector) PrepareAction(_ context.Context, request connectors.ActionRequest) (connectors.PreparedAction, error) {
	if request.ActionName != "echo" {
		return connectors.PreparedAction{}, fmt.Errorf("unsupported action %q", request.ActionName)
	}
	values, err := connectors.NormalizeSchemaValues(connectors.Schema{Fields: []connectors.Field{{Name: "message", Label: "Message", Type: connectors.FieldString, Required: true}}}, request.Input)
	if err != nil {
		return connectors.PreparedAction{}, err
	}
	message := strings.TrimSpace(values["message"].(string))
	return connectors.PreparedAction{
		ConnectorKind: "e2e",
		TargetRef:     request.Target.Ref,
		ProfileID:     request.Profile.ID,
		ActionName:    request.ActionName,
		Risk:          connectors.RiskRead,
		Title:         "Echo deterministic message",
		Summary:       "Return the approved message through the real connector pipeline.",
		Preview:       map[string]any{"message": message},
		Payload:       map[string]any{"message": message},
	}, nil
}
func (testConnector) ExecuteAction(_ context.Context, _ connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	message := fmt.Sprint(action.Payload["message"])
	output := map[string]any{"message": message}
	encoded, _ := json.Marshal(output)
	return connectors.ActionResult{Status: connectors.ResultCompleted, Output: output, DisplayText: string(encoded)}, nil
}
