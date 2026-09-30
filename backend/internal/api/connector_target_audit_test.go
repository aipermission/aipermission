package api

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/observability"
)

func TestConnectorTargetAuditPersistsThroughProductionCompositionWithoutRuntime(t *testing.T) {
	fixture := newAPITestFixture(t)
	target, err := connectortargets.NewStore(fixture.db).CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "alpha", Name: "Target-only audit fixture", Config: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	gateway := fixture.server.connectorRuntime.TargetOperationGateway(fixture.server.activeRuntime())(target.ConnectorKind, target.ID)
	if err := gateway.ConnectorWriteTargetAudit(t.Context(), "connector.fixture_attested", map[string]any{
		"target_id": int64(999), "connector_kind": "spoofed", "reason": "externally checked public evidence",
	}); err != nil {
		t.Fatal(err)
	}
	response := performJSON(fixture.server.Handler(), http.MethodGet,
		"/api/audit-logs?connector_kind=alpha&target_id="+strconv.FormatInt(target.ID, 10), "", nil)
	page := decodeRouteResponse[pageResponse[observability.Record]](t, response.Body.Bytes())
	if response.Code != http.StatusOK || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("scoped target audit dropped: %d %s", response.Code, response.Body.String())
	}
	entry := page.Items[0]
	if entry.Action != "connector.fixture_attested" || entry.TargetID == nil || *entry.TargetID != target.ID || entry.ConnectorKind != target.ConnectorKind || entry.TargetName != target.Name {
		t.Fatalf("audit identity came from caller payload: %#v", entry)
	}
	detail := performJSON(fixture.server.Handler(), http.MethodGet, "/api/audit-logs/"+strconv.FormatInt(entry.ID, 10), "", nil)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "externally checked public evidence") {
		t.Fatalf("target audit evidence missing: %d %s", detail.Code, detail.Body.String())
	}
	var count int
	if err := fixture.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM connector_runtime_surfaces").Scan(&count); err != nil || count != 0 {
		t.Fatalf("auditing created artificial execution state: count %d error %v", count, err)
	}
}
