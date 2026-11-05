package postgresconnector

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
	"github.com/jackc/pgx/v5/pgproto3"
)

func TestExecuteSQLClientCopyClosesPromptlyWithoutSuccessOrReplay(t *testing.T) {
	for _, response := range []pgproto3.BackendMessage{&pgproto3.CopyInResponse{}, &pgproto3.CopyOutResponse{}, &pgproto3.CopyBothResponse{}} {
		t.Run(fmt.Sprintf("%T", response), func(t *testing.T) {
			script := "BEGIN; UPDATE public.items SET value=7; COMMIT; COPY public.items TO STDOUT;"
			fixture := newExecuteWireFixture(t, script, func(peer *pgproto3.Backend) (byte, error) {
				peer.Send(&pgproto3.CommandComplete{CommandTag: []byte("UPDATE 1")})
				peer.Send(response)
				if _, output := response.(*pgproto3.CopyOutResponse); output {
					peer.Send(&pgproto3.CopyData{Data: []byte(strings.Repeat("x", 100000))})
				}
				if err := peer.Flush(); err != nil {
					return 'I', err
				}
				_, err := peer.Receive()
				return 'I', err
			})
			fixture.client = noDeadlineExecutionConn{fixture.client}
			fixture.start()
			runtime := cleanupFixtureRuntime(fixture)
			started := time.Now()
			result, err := New().ExecuteAction(t.Context(), runtime, prepareExecuteFixture(t, runtime, ActionExecuteSQL, map[string]any{"sql": script, "timeout_seconds": 300}))
			fixture.wait(t)
			assertExecuteUnknown(t, result, err)
			if time.Since(started) > 2*time.Second || !strings.Contains(result.Error, "COPY client transfer is not supported") || fixture.scripts != 1 || fixture.dials.Load() != 1 {
				t.Fatalf("COPY hung, retried or hid unsupported transfer: %#v peer=%v", result, fixture.err)
			}
			commands := result.Output.(map[string]any)["commands"].([]sqlexecute.Command)
			if len(commands) != 1 || commands[0].Tag != "UPDATE 1" {
				t.Fatalf("pre-COPY acknowledgement lost: %#v", result.Output)
			}
		})
	}
}
