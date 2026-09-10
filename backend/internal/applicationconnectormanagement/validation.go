package applicationconnectormanagement

import (
	"context"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func ValidateTransport(ctx context.Context, store *connectortargets.Store, projectID int64, config map[string]any, hasTCP func(string) bool) error {
	mode, _ := config["connection_mode"].(string)
	mode = strings.TrimSpace(mode)
	if !connectors.UsesConnectorTransport(mode, "direct") {
		return nil
	}
	ref, _ := config["transport_target_ref"].(string)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return connectortargets.ValidationError(fmt.Sprintf("transport target ref is required for connection mode %q", mode))
	}
	if err := store.ValidateTransportProject(ctx, projectID, ref); err != nil {
		return err
	}
	kind, _, _, ok := connectors.ParseTargetRef(ref)
	if !ok {
		return connectortargets.ErrInvalidTargetRef
	}
	if hasTCP == nil || !hasTCP(kind) {
		return connectortargets.ValidationError(fmt.Sprintf("%s connector does not expose reviewed TCP transport", kind))
	}
	return nil
}
