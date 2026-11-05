package sqlexecute_test

import (
	"fmt"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
)

func TestCollectorReservesMaximumEnvelopeMetadata(t *testing.T) {
	c := sqlexecute.NewCollector(1000)
	columns, values := make([]string, 256), make([]any, 256)
	for i := range columns {
		columns[i] = fmt.Sprintf("c%d", i)
		values[i] = make([]int, 11)
	}
	for range 32 {
		if err := c.BeginResult(columns); err != nil {
			t.Fatal(err)
		}
		if err := c.AddRow(values); err != nil {
			t.Fatal(err)
		}
		c.EndResult()
	}
	for range 128 {
		c.Command("UPDATE 1", 1)
	}
	for range 32 {
		c.Notice("bounded notice")
	}
	out := c.Output(1)
	if _, err := actionresult.Canonicalize(out, actionresult.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if out["truncated"] != true {
		t.Fatal("structural row omissions were hidden")
	}
}

func TestCollectorDepthIncludesItsCompleteEnvelope(t *testing.T) {
	for _, depth := range []int{26, 27} {
		c := sqlexecute.NewCollector(1)
		if err := c.BeginResult([]string{"value"}); err != nil {
			t.Fatal(err)
		}
		var value any = 1
		for range depth {
			value = []any{value}
		}
		if err := c.AddRow([]any{value}); err != nil {
			t.Fatal(err)
		}
		c.EndResult()
		out := c.Output(1)
		if _, err := actionresult.Canonicalize(out, actionresult.DefaultLimits()); err != nil {
			t.Fatal(err)
		}
		if out["truncated"] != (depth == 27) {
			t.Fatalf("depth %d, truncated=%v", depth, out["truncated"])
		}
	}
}

func TestCollectorRetainsOnlyCanonicallyBoundedRows(t *testing.T) {
	for _, name := range []string{"nodes", "depth", "multiple_results"} {
		t.Run(name, func(t *testing.T) {
			c := sqlexecute.NewCollector(1000)
			sets, rows := 1, 8
			var value any = make([]float64, 19000)
			if name == "depth" {
				value, rows = 1, 1
				for range 30 {
					value = []any{value}
				}
			} else if name == "multiple_results" {
				sets, rows = 8, 1
			}
			for range sets {
				if err := c.BeginResult([]string{"value"}); err != nil {
					t.Fatal(err)
				}
				for range rows {
					if err := c.AddRow([]any{value}); err != nil {
						t.Fatal(err)
					}
				}
				c.EndResult()
			}
			c.Command("UPDATE 1", 1)
			out := c.Output(1)
			if _, err := actionresult.Canonicalize(out, actionresult.DefaultLimits()); err != nil {
				t.Fatal(err)
			}
			if out["truncated"] != true || out["completed_batches"] != 1 {
				t.Fatalf("structural truncation or completion lost: %#v", out)
			}
		})
	}
}
