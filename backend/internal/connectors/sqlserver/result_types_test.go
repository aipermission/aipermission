package sqlserverconnector

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type inspectedFixture struct {
	resultFixture
	metadata  *resultFixture
	steps     []string
	arguments []driver.NamedValue
}

func (f *inspectedFixture) Connect(context.Context) (driver.Conn, error) { return f, nil }
func (f *inspectedFixture) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	f.steps = append(f.steps, query)
	return driver.RowsAffected(0), nil
}
func (f *inspectedFixture) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	f.steps = append(f.steps, query)
	if query == describeResultSQL {
		f.arguments = args
		if f.metadata.queryErr != nil {
			return nil, f.metadata.queryErr
		}
		return f.metadata, nil
	}
	return &f.resultFixture, nil
}

func TestResultInspectionPrecedesTargetExecutionAndRowLimit(t *testing.T) {
	for _, test := range []struct {
		name     string
		data     [][]driver.Value
		err      error
		accepted bool
	}{
		{"safe alias scalar", [][]driver.Value{{int64(1), int64(56), nil, nil}}, nil, true},
		{"xml third column", [][]driver.Value{{int64(1), int64(56), nil, nil}, {int64(2), int64(56), nil, nil}, {int64(3), int64(241), nil, nil}}, nil, false},
		{"variant", [][]driver.Value{{int64(1), int64(98), nil, nil}}, nil, false},
		{"clr", [][]driver.Value{{int64(1), int64(240), nil, nil}}, nil, false},
		{"unknown", [][]driver.Value{{int64(1), int64(999), nil, nil}}, nil, false},
		{"null type", [][]driver.Value{{int64(1), nil, nil, nil}}, nil, false},
		{"missing ordinal", [][]driver.Value{{int64(2), int64(56), nil, nil}}, nil, false},
		{"metadata error", [][]driver.Value{{nil, nil, int64(115), int64(1)}}, nil, false},
		{"empty", nil, nil, false},
		{"stream failure", nil, errors.New("fixture metadata stream failure"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := &inspectedFixture{metadata: &resultFixture{columns: []string{"ordinal", "type", "error", "error_type"}, data: test.data, rowErr: test.err}, resultFixture: resultFixture{columns: []string{"value"}, types: []string{"INT"}, data: [][]driver.Value{{int64(1)}}}}
			db := sql.OpenDB(f)
			defer db.Close()
			_, err := queryRows(t.Context(), db, "SELECT 1", 1, true)
			if (err == nil) != test.accepted {
				t.Fatalf("error=%v", err)
			}
			want := []string{"SET ROWCOUNT 0", describeResultSQL}
			if test.accepted {
				want = append(want, "SET ROWCOUNT 2", "SELECT 1")
			}
			if !reflect.DeepEqual(f.steps, want) || len(f.arguments) != 1 || f.arguments[0].Name != "tsql" || f.arguments[0].Value != "SELECT 1" {
				t.Fatalf("dispatch=%v arguments=%v", f.steps, f.arguments)
			}
		})
	}
}

func TestResultInspectionRejectsExcessiveColumns(t *testing.T) {
	for _, count := range []int{256, 257} {
		metadata := &resultFixture{columns: []string{"ordinal", "type", "error", "error_type"}}
		for i := 1; i <= count; i++ {
			metadata.data = append(metadata.data, []driver.Value{int64(i), int64(56), nil, nil})
		}
		f := &inspectedFixture{metadata: metadata, resultFixture: resultFixture{columns: []string{"value"}, types: []string{"INT"}}}
		db := sql.OpenDB(f)
		_, err := queryRows(t.Context(), db, "SELECT 1", 1, true)
		_ = db.Close()
		if (err == nil) != (count == 256) {
			t.Fatalf("columns=%d error=%v", count, err)
		}
	}
}

func TestTemporalValuesPreserveTheirSQLTypeSemantics(t *testing.T) {
	value := time.Date(2026, 11, 2, 13, 14, 15, 123456700, time.FixedZone("fixture", 5*3600+30*60))
	for kind, want := range map[string]string{
		"DATE": "2026-11-02", "TIME": "13:14:15.1234567",
		"DATETIME": "2026-11-02T13:14:15.1234567", "SMALLDATETIME": "2026-11-02T13:14:15.1234567", "DATETIME2": "2026-11-02T13:14:15.1234567",
		"DATETIMEOFFSET": "2026-11-02T13:14:15.1234567+05:30",
	} {
		if got := normalizeValue(value, kind); got != want {
			t.Errorf("%s=%v want=%s", kind, got, want)
		}
		if normalizeValue(nil, kind) != nil {
			t.Errorf("%s NULL changed", kind)
		}
	}
	if got := normalizeValue([]byte(strings.Repeat("x", 10*maxCellBytes)), "VARBINARY").(string); len(got) > maxCellBytes+4 || len(got) <= maxCellBytes {
		t.Fatalf("binary normalization allocation/truncation=%d", len(got))
	}
}

func TestActionOutputRowHintsMatchExecutionLimits(t *testing.T) {
	actions, err := New().GetActionList(t.Context(), connectors.TargetView{}, connectors.CredentialProfileView{})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range actions {
		limit := maxRows
		if action.Name == ActionGetMetadata {
			limit = maxMetadataRows
		}
		if action.OutputHint.MaxRows != limit {
			t.Errorf("%s advertises %d instead of %d", action.Name, action.OutputHint.MaxRows, limit)
		}
	}
	if !strings.Contains(columnsSQL, "c.max_length AS max_length_bytes") {
		t.Fatal("column storage length unit is ambiguous")
	}
}
