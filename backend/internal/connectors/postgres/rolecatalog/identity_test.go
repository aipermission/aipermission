package rolecatalog

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/jackc/pgx/v5"
)

func TestCapturePreservesClusterBitsAndExactNames(t *testing.T) {
	record := testRecord()
	local := record.Intent.Anchor
	local.ClusterID, local.DatabaseOID, local.SuccessorOID = "", 0, 0
	got, err := CaptureAnchor(t.Context(), validTransaction(t, record), local)
	if err != nil || got != record.Intent.Anchor {
		t.Fatalf("remote identity normalized/lost: %#v %v", got, err)
	}
	for raw, want := range map[string]string{"1": "1", "9223372036854775807": "9223372036854775807", "-9223372036854775808": "9223372036854775808", "-1": "18446744073709551615"} {
		if got, err := clusterIdentifier(raw); err != nil || got != want {
			t.Fatalf("cluster %q: %q %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "0", "-0", "01", "+1", " 1", "1.0", "9223372036854775808", "-9223372036854775809"} {
		if _, err := clusterIdentifier(raw); err == nil {
			t.Fatalf("invalid signed system identifier accepted: %q", raw)
		}
	}
}

func TestCaptureFailsClosedOnInvalidAuthorityOrRead(t *testing.T) {
	record := testRecord()
	tx := validTransaction(t, record)
	local := record.Intent.Anchor
	local.TargetID = 0
	if _, err := CaptureAnchor(t.Context(), tx, local); err == nil || len(tx.queries) != 0 {
		t.Fatal("invalid authority queried")
	}
	if _, err := CaptureAnchor(t.Context(), nil, record.Intent.Anchor); err == nil {
		t.Fatal("nil transaction accepted")
	}
	fault := errors.New("catalog read denied")
	tx.rows[anchorQuery] = func(...any) error { return fault }
	if _, err := CaptureAnchor(t.Context(), tx, record.Intent.Anchor); !errors.Is(err, fault) {
		t.Fatal("catalog error ignored")
	}
	for _, mode := range []string{"cluster", "zero database", "zero admin", "database name", "admin name"} {
		t.Run(mode, func(t *testing.T) {
			tx := validTransaction(t, record)
			original := tx.rows[anchorQuery]
			tx.rows[anchorQuery] = func(destination ...any) error {
				if err := original(destination...); err != nil {
					return err
				}
				switch mode {
				case "cluster":
					*destination[0].(*string) = "invalid"
				case "zero database":
					*destination[1].(*uint32) = 0
				case "zero admin":
					*destination[3].(*uint32) = 0
				case "database name":
					*destination[2].(*string) = "Main DB"
				case "admin name":
					*destination[4].(*string) = "Main Admin"
				}
				return nil
			}
			if _, err := CaptureAnchor(t.Context(), tx, record.Intent.Anchor); err == nil {
				t.Fatal("remote drift accepted")
			}
		})
	}
}

func TestVerifyRoleChecksWholeRemoteIdentityAndExactMarker(t *testing.T) {
	record := testRecord()
	tx := validTransaction(t, record)
	if err := VerifyRole(t.Context(), tx, record); err != nil {
		t.Fatal(err)
	}
	if len(tx.queries) != 2 || tx.queries[0] != anchorQuery || tx.queries[1] != roleQuery ||
		len(tx.arguments[0]) != 0 || len(tx.arguments[1]) != 1 || tx.arguments[1][0] != record.Intent.RoleName {
		t.Fatalf("exact parameterized role identity not checked: %#v", tx)
	}
	for _, mode := range []string{"cluster", "database OID", "successor OID", "role OID", "role name", "marker", "absent"} {
		t.Run(mode, func(t *testing.T) {
			tx := validTransaction(t, record)
			query := roleQuery
			if mode == "cluster" || mode == "database OID" || mode == "successor OID" {
				query = anchorQuery
			}
			original := tx.rows[query]
			tx.rows[query] = func(destination ...any) error {
				if mode == "absent" {
					return pgx.ErrNoRows
				}
				if err := original(destination...); err != nil {
					return err
				}
				switch mode {
				case "cluster":
					*destination[0].(*string) = "123"
				case "database OID":
					*destination[1].(*uint32) = 13
				case "successor OID":
					*destination[3].(*uint32) = 11
				case "role OID":
					*destination[0].(*uint32) = 43
				case "role name":
					*destination[1].(*string) = "My Role"
				case "marker":
					*destination[2].(*string) = record.Intent.Marker() + "-other"
				}
				return nil
			}
			if err := VerifyRole(t.Context(), tx, record); !errors.Is(err, ErrIdentityDrift) {
				t.Fatalf("remote drift accepted: %v", err)
			}
			if query == anchorQuery && len(tx.queries) != 1 {
				t.Fatal("role queried after anchor drift")
			}
		})
	}
}

func TestVerifyRoleRejectsUnboundMalformedAndFailedReads(t *testing.T) {
	record := testRecord()
	for _, mutate := range []func(*rolejournal.Record){func(r *rolejournal.Record) { r.Version = 99 }, func(r *rolejournal.Record) { r.RoleOID = 0; r.Status = rolejournal.ProvisionIntent }} {
		tx := validTransaction(t, record)
		invalid := record
		mutate(&invalid)
		if err := VerifyRole(t.Context(), tx, invalid); err == nil || len(tx.queries) != 0 {
			t.Fatal("invalid identity queried")
		}
	}
	tx := validTransaction(t, record)
	fault := errors.New("role read denied")
	tx.rows[roleQuery] = func(...any) error { return fault }
	if err := VerifyRole(t.Context(), tx, record); !errors.Is(err, fault) {
		t.Fatal("role read failure ignored")
	}
	tx = validTransaction(t, record)
	tx.rows[anchorQuery] = func(...any) error { return fault }
	if err := VerifyRole(t.Context(), tx, record); !errors.Is(err, fault) || len(tx.queries) != 1 {
		t.Fatal("anchor failure ignored")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := VerifyRole(ctx, validTransaction(t, record), record); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
}
