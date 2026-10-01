package apiadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

func TestRoleHistoryCursorStrictDecoding(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
		id    int64
		valid bool
	}{
		{name: "empty object", value: map[string]any{}, valid: true},
		{name: "zero", value: map[string]any{"after_resource_id": "0"}, valid: true},
		{name: "positive", value: map[string]any{"after_resource_id": "42"}, id: 42, valid: true},
		{name: "beyond float precision", value: map[string]any{"after_resource_id": "9007199254740993"}, id: 9007199254740993, valid: true},
		{name: "maximum int64", value: map[string]any{"after_resource_id": "9223372036854775807"}, id: 9223372036854775807, valid: true},
		{name: "nil"},
		{name: "nil object", value: map[string]any(nil)},
		{name: "string", value: "{}"},
		{name: "array", value: []any{}},
		{name: "typed map", value: map[string]string{"after_resource_id": "1"}},
		{name: "unknown field", value: map[string]any{"after": "1"}},
		{name: "extra field", value: map[string]any{"after_resource_id": "1", "target_id": 41}},
		{name: "null cursor", value: map[string]any{"after_resource_id": nil}},
		{name: "integer cursor", value: map[string]any{"after_resource_id": int64(1)}},
		{name: "JSON numeric cursor", value: map[string]any{"after_resource_id": float64(1)}},
		{name: "JSON number cursor", value: map[string]any{"after_resource_id": json.Number("1")}},
		{name: "boolean cursor", value: map[string]any{"after_resource_id": true}},
		{name: "empty cursor", value: map[string]any{"after_resource_id": ""}},
		{name: "leading whitespace", value: map[string]any{"after_resource_id": " 1"}},
		{name: "trailing whitespace", value: map[string]any{"after_resource_id": "1\n"}},
		{name: "plus sign", value: map[string]any{"after_resource_id": "+1"}},
		{name: "negative", value: map[string]any{"after_resource_id": "-1"}},
		{name: "negative zero", value: map[string]any{"after_resource_id": "-0"}},
		{name: "leading zero", value: map[string]any{"after_resource_id": "01"}},
		{name: "multiple zeroes", value: map[string]any{"after_resource_id": "00"}},
		{name: "fraction", value: map[string]any{"after_resource_id": "1.0"}},
		{name: "exponent", value: map[string]any{"after_resource_id": "1e3"}},
		{name: "hexadecimal", value: map[string]any{"after_resource_id": "0x10"}},
		{name: "separator", value: map[string]any{"after_resource_id": "1_000"}},
		{name: "non ASCII digits", value: map[string]any{"after_resource_id": "\u0661"}},
		{name: "overflow", value: map[string]any{"after_resource_id": "9223372036854775808"}},
		{name: "unsigned maximum", value: map[string]any{"after_resource_id": "18446744073709551615"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, valid := roleHistoryCursor(test.value)
			if valid != test.valid || valid && id != test.id {
				t.Fatalf("cursor = (%d, %t), want (%d, %t)", id, valid, test.id, test.valid)
			}
			store := newRoleHistoryStore(t)
			runtime := &roleHistoryRuntime{scopedRuntime: scopedRuntime{store: store}}
			response, err := (adapter{}).RunTargetOperation(t.Context(), roleHistoryGateway{t: t}, runtime, roleHistoryTarget(), RoleHistoryOperation, test.value)
			if test.valid {
				if err != nil || response.StatusCode != http.StatusOK {
					t.Fatalf("valid cursor failed: %#v, %v", response, err)
				}
				assertRoleHistoryReads(t, runtime, store, t.Context(), 1, 1)
			} else {
				assertRoleHistoryError(t, response, err, http.StatusBadRequest, "invalid managed Postgres role history request")
				assertRoleHistoryReads(t, runtime, store, nil, 0, 0)
			}
		})
	}
}

func TestRoleHistoryUnsupportedOperationPrecedesInspection(t *testing.T) {
	for _, operation := range []string{"", "unknown", "ROLE-LIFECYCLE-STATUS", RoleHistoryOperation + " "} {
		t.Run(operation, func(t *testing.T) {
			store := newRoleHistoryStore(t)
			runtime := &roleHistoryRuntime{scopedRuntime: scopedRuntime{store: store}}
			response, err := (adapter{}).RunTargetOperation(nil, roleHistoryGateway{t: t}, runtime, connectorapi.Target{}, operation, nil)
			assertRoleHistoryError(t, response, err, http.StatusBadRequest, "unsupported connector operation")
			assertRoleHistoryReads(t, runtime, store, nil, 0, 0)
		})
	}
}

func TestRoleHistoryUnavailableEvidenceIsSanitizedAndReadOnly(t *testing.T) {
	for _, failure := range []string{
		"missing runtime", "typed nil runtime", "missing resource scope", "typed nil resource scope", "missing context", "missing target", "negative target", "missing connector kind", "wrong connector kind",
		"canceled context", "expired context", "read failure", "malformed record", "malformed foreign record",
	} {
		t.Run(failure, func(t *testing.T) {
			store := newRoleHistoryStore(t)
			runtime := &roleHistoryRuntime{scopedRuntime: scopedRuntime{store: store}}
			var dataRuntime connectorapi.ConnectorDataRuntime = runtime
			ctx, target := t.Context(), roleHistoryTarget()
			scopes, lists := 0, 0
			message := "managed Postgres role history target is unavailable"
			switch failure {
			case "missing runtime":
				dataRuntime = nil
			case "typed nil runtime":
				dataRuntime = (*roleHistoryRuntime)(nil)
			case "missing resource scope":
				runtime.store = nil
				scopes = 1
			case "typed nil resource scope":
				runtime.store = (*roleHistoryStore)(nil)
				scopes = 1
			case "missing context":
				ctx = nil
			case "missing target":
				target.ID = 0
			case "negative target":
				target.ID = -1
			case "missing connector kind":
				target.ConnectorKind = ""
			case "wrong connector kind":
				target.ConnectorKind = "ssh"
			case "canceled context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				scopes, lists = 1, 1
			case "expired context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Unix(0, 0))
				defer cancel()
				scopes, lists = 1, 1
			case "read failure":
				seedRoleHistoryEntry(t, store, target.ID)
				store.listErr = errors.New("private connection password must not reach response")
				scopes, lists = 1, 1
			case "malformed record", "malformed foreign record":
				targetID := target.ID
				if failure == "malformed foreign record" {
					targetID++
				}
				seedRoleHistoryEntry(t, store, targetID)
				store.rows[0].PublicData = "private invalid evidence must not reach response"
				scopes, lists = 1, 1
			}
			if scopes != 0 {
				message = "managed Postgres role evidence could not be inspected; reload before retrying"
			}
			store.readOnly = true
			before := slices.Clone(store.rows)
			response, err := (adapter{}).RunTargetOperation(ctx, roleHistoryGateway{t: t}, dataRuntime, target, RoleHistoryOperation, map[string]any{})
			assertRoleHistoryError(t, response, err, http.StatusConflict, message)
			assertRoleHistoryReads(t, runtime, store, ctx, scopes, lists)
			if !reflect.DeepEqual(store.rows, before) {
				t.Fatal("failed history inspection changed durable evidence")
			}
		})
	}
}

func TestRoleHistoryBoundedPageSerialization(t *testing.T) {
	for _, test := range []struct {
		name       string
		count      int
		afterCount int
		firstID    int64
		nilGateway bool
	}{
		{name: "empty"},
		{name: "one entry", count: 1},
		{name: "below limit", count: rolejournal.HistoryPageLimit - 1},
		{name: "exact limit with foreign entries", count: rolejournal.HistoryPageLimit},
		{name: "over limit", count: rolejournal.HistoryPageLimit + 1},
		{name: "multiple pages", count: 2*rolejournal.HistoryPageLimit + 1},
		{name: "middle page", count: 2*rolejournal.HistoryPageLimit + 1, afterCount: rolejournal.HistoryPageLimit},
		{name: "final page", count: rolejournal.HistoryPageLimit + 1, afterCount: rolejournal.HistoryPageLimit},
		{name: "cursor at last entry", count: 3, afterCount: 3},
		{name: "large resource IDs", count: rolejournal.HistoryPageLimit + 1, firstID: 9007199254740993},
		{name: "large cursor with continuation", count: rolejournal.HistoryPageLimit + 2, afterCount: 1, firstID: 9007199254740993},
		{name: "no gateway required", count: 1, nilGateway: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newRoleHistoryStore(t)
			if test.firstID != 0 {
				store.nextID = test.firstID
			}
			target := roleHistoryTarget()
			seedRoleHistoryEntry(t, store, target.ID+1)
			entries := make([]rolejournal.Entry, 0, test.count)
			for range test.count {
				entries = append(entries, seedRoleHistoryEntry(t, store, target.ID))
			}
			seedRoleHistoryEntry(t, store, target.ID+1)
			slices.Reverse(store.rows)
			before := slices.Clone(store.rows)
			store.readOnly = true
			input := map[string]any{}
			if test.afterCount != 0 {
				input["after_resource_id"] = strconv.FormatInt(entries[test.afterCount-1].ResourceID, 10)
			}
			remaining := entries[test.afterCount:]
			hasMore := len(remaining) > rolejournal.HistoryPageLimit
			next := ""
			if hasMore {
				remaining = remaining[:rolejournal.HistoryPageLimit]
				next = strconv.FormatInt(remaining[len(remaining)-1].ResourceID, 10)
			}
			runtime := &roleHistoryRuntime{scopedRuntime: scopedRuntime{store: store}}
			var gateway connectorapi.TargetOperationGateway = roleHistoryGateway{t: t}
			if test.nilGateway {
				gateway = nil
			}
			response, err := (adapter{}).RunTargetOperation(t.Context(), gateway, runtime, target, RoleHistoryOperation, input)
			if err != nil || response.StatusCode != http.StatusOK {
				t.Fatalf("inspect history: %#v, %v", response, err)
			}
			encoded, err := json.Marshal(response.Payload)
			if err != nil {
				t.Fatal(err)
			}
			// Compare the public wire shape independently of the response's embedded type.
			want, err := json.Marshal(map[string]any{
				"target_id": target.ID, "entries": remaining, "has_more": hasMore, "next_after_resource_id": next,
			})
			if err != nil {
				t.Fatal(err)
			}
			var gotJSON, wantJSON map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &gotJSON); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(want, &wantJSON); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotJSON, wantJSON) {
				t.Fatalf("serialized history differs: got %s, want %s", encoded, want)
			}
			var wireEntries []struct {
				ResourceID string `json:"resource_id"`
			}
			if err := json.Unmarshal(gotJSON["entries"], &wireEntries); err != nil {
				t.Fatalf("resource IDs must serialize as decimal strings: %v", err)
			}
			for index, entry := range wireEntries {
				if entry.ResourceID != strconv.FormatInt(remaining[index].ResourceID, 10) {
					t.Fatalf("resource ID lost precision: got %q, want %d", entry.ResourceID, remaining[index].ResourceID)
				}
			}
			if !reflect.DeepEqual(store.rows, before) {
				t.Fatal("history inspection changed durable evidence")
			}
			assertRoleHistoryReads(t, runtime, store, t.Context(), 1, 1)
		})
	}
}

func roleHistoryTarget() connectorapi.Target {
	return connectorapi.Target{ID: 41, ConnectorKind: postgresconnector.Kind, Config: map[string]any{"password": "private target password"}}
}

func assertRoleHistoryError(t *testing.T, response connectors.ManagementResponse, err error, status int, message string) {
	t.Helper()
	if err != nil || response.StatusCode != status || !reflect.DeepEqual(response.Payload, map[string]string{"error": message}) {
		t.Fatalf("response = %#v, error = %v; want status %d and sanitized error %q", response, err, status, message)
	}
}

func assertRoleHistoryReads(t *testing.T, runtime *roleHistoryRuntime, store *roleHistoryStore, ctx context.Context, scopes, lists int) {
	t.Helper()
	wantKinds := []string(nil)
	if scopes != 0 {
		wantKinds = []string{rolejournal.ResourceKind}
	}
	if !reflect.DeepEqual(runtime.kinds, wantKinds) || store.lists != lists || lists != 0 && store.listContext != ctx {
		t.Fatalf("unexpected resource access: kinds %v, lists %d, context %v; want %v, %d, %v", runtime.kinds, store.lists, store.listContext, wantKinds, lists, ctx)
	}
}

type roleHistoryRuntime struct {
	// Nil embedded methods trap any target/profile/runtime authority beyond the scoped store.
	connectorapi.ConnectorDataRuntime
	scopedRuntime
}

func (runtime *roleHistoryRuntime) CredentialResources(kind string) resourcecontract.CredentialResourceStore {
	return runtime.scopedRuntime.CredentialResources(kind)
}

type roleHistoryGateway struct {
	// Remote command and console methods remain nil traps as well.
	connectorapi.LiveConsoleGateway
	t *testing.T
}

func (gateway roleHistoryGateway) ConnectorTrustStorePath() string {
	gateway.t.Fatal("history inspection requested peer identity authority")
	return ""
}

func (gateway roleHistoryGateway) ConnectorWriteAudit(context.Context, string, *int64, int64, string, any) {
	gateway.t.Fatal("history inspection wrote a runtime audit")
}

func (gateway roleHistoryGateway) ConnectorWriteTargetAudit(context.Context, string, any) error {
	gateway.t.Fatal("history inspection wrote a target audit")
	return nil
}

type roleHistoryStore struct {
	// All unimplemented methods (secret, update, delete, reference counts) are forbidden.
	resourcecontract.CredentialResourceStore
	t           *testing.T
	rows        []resourcecontract.CredentialResource
	nextID      int64
	readOnly    bool
	listErr     error
	lists       int
	listContext context.Context
}

func newRoleHistoryStore(t *testing.T) *roleHistoryStore {
	return &roleHistoryStore{t: t, nextID: 1, readOnly: true}
}

func (store *roleHistoryStore) List(ctx context.Context) ([]resourcecontract.CredentialResource, error) {
	if store.readOnly {
		store.lists++
		store.listContext = ctx
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return slices.Clone(store.rows), store.listErr
}

func (store *roleHistoryStore) Create(ctx context.Context, input resourcecontract.CreateCredentialResourceInput) (resourcecontract.CredentialResource, error) {
	if store.readOnly {
		store.t.Fatal("history inspection created journal evidence")
	}
	if err := ctx.Err(); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	row := resourcecontract.CredentialResource{
		ID: store.nextID, Name: input.Name, ResourceType: input.ResourceType, PublicData: input.PublicData, Fingerprint: input.Fingerprint,
		CreatedAt: "private storage metadata", UpdatedAt: "private storage metadata",
	}
	store.nextID++
	store.rows = append(store.rows, row)
	return row, nil
}

func (store *roleHistoryStore) Get(ctx context.Context, id int64) (resourcecontract.CredentialResource, error) {
	if store.readOnly {
		store.t.Fatal("history inspection requested individual journal evidence")
	}
	if err := ctx.Err(); err != nil {
		return resourcecontract.CredentialResource{}, err
	}
	for _, row := range store.rows {
		if row.ID == id {
			return row, nil
		}
	}
	return resourcecontract.CredentialResource{}, resourcecontract.ErrCredentialResourceNotFound
}

func seedRoleHistoryEntry(t *testing.T, store *roleHistoryStore, targetID int64) rolejournal.Entry {
	t.Helper()
	store.readOnly = false
	defer func() { store.readOnly = true }()
	entry, err := rolejournal.New(store).BeginProvision(t.Context(), rolejournal.Anchor{
		TargetID: targetID, ContextDigest: strings.Repeat("a", 64), AdminProfileID: 2,
		TargetDigest: strings.Repeat("c", 64),
		ClusterID:    "18446744073709551615", DatabaseOID: 12, DatabaseName: "main",
		SuccessorOID: 10, SuccessorName: "admin",
	}, fmt.Sprintf("role_%d", store.nextID))
	if err != nil {
		t.Fatal(err)
	}
	return entry
}
