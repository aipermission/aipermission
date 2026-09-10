package connectormanagement

import (
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

type connectorCatalogItem struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	Version string `json:"version"`
}

type connectorCatalogDetail struct {
	Kind              string                        `json:"kind"`
	Label             string                        `json:"label"`
	Version           string                        `json:"version"`
	TargetSchema      connectors.Schema             `json:"target_schema"`
	CredentialSchemas []connectors.CredentialSchema `json:"credential_schemas"`
	Help              connectors.ConnectorHelp      `json:"help"`
}

func (h *HTTPHandlers) ListConnectors(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireRegistry)
	if !ok {
		return
	}
	infos := scope.Registry.List()
	items := make([]connectorCatalogItem, 0, len(infos))
	for _, info := range infos {
		items = append(items, connectorCatalogItem{
			Kind:    info.Kind,
			Label:   info.Label,
			Version: info.Version,
		})
	}
	httptransport.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *HTTPHandlers) GetConnector(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolve(w, requireRegistry)
	if !ok {
		return
	}
	kind := r.PathValue("kind")
	if !connectors.ValidIdentifier(kind) {
		httptransport.WriteError(w, http.StatusBadRequest, "invalid connector kind")
		return
	}
	connector, ok := scope.Registry.Get(kind)
	if !ok {
		httptransport.WriteError(w, http.StatusNotFound, "connector not found")
		return
	}
	if err := connectors.ValidateNonSecretSchema(connector.TargetSchema(), connector.Kind()+" target"); err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	target := connectors.TargetView{ConnectorKind: connector.Kind()}
	help, err := connector.GetHelp(r.Context(), target)
	if err != nil {
		httptransport.WriteInternalError(w)
		return
	}
	httptransport.WriteJSON(w, http.StatusOK, connectorCatalogDetail{
		Kind:              connector.Kind(),
		Label:             connector.Label(),
		Version:           connector.Version(),
		TargetSchema:      connector.TargetSchema(),
		CredentialSchemas: connector.CredentialSchemas(),
		Help:              help,
	})
}
