package postgresconnector

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
)

func TestQueryRowsRejectsMalformedWireFieldCounts(t *testing.T) {
	for _, count := range []int{0, 1, 4} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			const sql = "SELECT first, second, third FROM public.items"
			f := newExecuteWireFixture(t, sql, func(b *pgproto3.Backend) (byte, error) {
				fields := []pgproto3.FieldDescription{executeTextField("first"), executeTextField("second"), executeTextField("third")}
				values := make([][]byte, count)
				for i := range values {
					values[i] = []byte("fixture")
				}
				sendExecuteRows(b, fields, values)
				return 'T', nil
			})
			f.start()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			conn, err := connect(ctx, cleanupFixtureRuntime(f))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(ctx)
			tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
			if err != nil {
				t.Fatal(err)
			}
			result, err := queryRows(ctx, tx, sql, 10, pgx.QueryExecModeSimpleProtocol)
			if err == nil || !strings.Contains(err.Error(), "number of field descriptions must equal number of values") || len(result.Rows) != 0 {
				t.Fatalf("malformed row returned partial success: %#v error=%v", result, err)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if err := conn.Close(ctx); err != nil {
				t.Fatal(err)
			}
			f.wait(t)
			if f.err != nil || f.scripts != 1 || f.dials.Load() != 1 {
				t.Fatalf("malformed reply retried or retained its connection: peer=%v scripts=%d dials=%d", f.err, f.scripts, f.dials.Load())
			}
		})
	}
}
