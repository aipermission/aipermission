package conformance_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	redisconnector "github.com/aipermission/aipermission/backend/internal/connectors/redis"
)

func TestValkeyBinaryKeyIdentityRealService(t *testing.T) {
	requireConformance(t)
	runtime := valkeyFixtureRuntime(t)
	connector := redisconnector.New()
	assertConnection(t, connector, runtime)
	prefix := fmt.Sprintf("aipermission:conformance:identity:%d:", time.Now().UnixNano())
	binary, literal := prefix+"\xff", prefix+"\ufffd"
	t.Cleanup(func() {
		for _, key := range []string{binary, literal} {
			redisIdentityFixtureExchange(t, runtime, []string{"DEL", key}, ":0\r\n", ":1\r\n")
		}
	})
	redisIdentityFixtureExchange(t, runtime, []string{"SET", binary, "binary-owned", "EX", "60"}, "+OK\r\n")
	executeAction(t, connector, runtime, redisconnector.ActionSetString, map[string]any{"key": literal, "value": "literal-owned", "ttl_seconds": 60})
	prepared, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{
		Target: runtime.Target, Profile: runtime.Profile, ActionName: redisconnector.ActionScanKeys,
		Input: map[string]any{"pattern": prefix + "*"}, Reason: "owned binary key identity fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := connector.ExecuteAction(t.Context(), runtime, prepared)
	if err == nil || !strings.Contains(err.Error(), "UTF-8") || result.Output != nil || result.DisplayText != "" {
		t.Fatalf("binary SCAN identity was published: %#v / %v", result, err)
	}
	// A narrower MATCH excludes the binary sibling without rewriting or
	// skipping any identity in the returned page.
	result = executeAction(t, connector, runtime, redisconnector.ActionScanKeys, map[string]any{"pattern": literal})
	projected, err := actionresult.Canonicalize(result.Output, actionresult.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(projected)
	if err != nil {
		t.Fatal(err)
	}
	var reply struct{ Keys []string }
	if err := json.Unmarshal(encoded, &reply); err != nil || len(reply.Keys) != 1 || reply.Keys[0] != literal {
		t.Fatalf("literal key JSON identity changed: %#v / %v", reply.Keys, err)
	}
	key := reply.Keys[0]
	executeAction(t, connector, runtime, redisconnector.ActionGetKey, map[string]any{"key": key})
	executeAction(t, connector, runtime, redisconnector.ActionSetString, map[string]any{"key": key, "value": "replacement-owned", "ttl_seconds": 60})
	executeAction(t, connector, runtime, redisconnector.ActionExpireKey, map[string]any{"key": key, "ttl_seconds": 30})
	executeAction(t, connector, runtime, redisconnector.ActionDeleteKeys, map[string]any{"keys": []any{key}})
	redisIdentityFixtureExchange(t, runtime, []string{"GET", binary}, "$12\r\nbinary-owned\r\n")
	redisIdentityFixtureExchange(t, runtime, []string{"GET", literal}, "$-1\r\n")
}

func redisIdentityFixtureExchange(t *testing.T, runtime connectors.RuntimeContext, args []string, replies ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	transport := directNetworkTransport{}
	conn, err := transport.DialConnectorTCP(ctx, connectors.NetworkDialRequest{
		Host: runtime.Target.Config["host"].(string), Port: runtime.Target.Config["port"].(int),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	password, err := runtime.Secrets.GetSecret(ctx, "password")
	if err != nil {
		t.Fatal(err)
	}
	redisIdentityFixtureRoundTrip(t, conn, []string{"AUTH", password}, "+OK\r\n")
	redisIdentityFixtureRoundTrip(t, conn, args, replies...)
}

// The fixture checks exact bounded RESP replies, not a second protocol parser.
// It seeds/removes only its random-prefix keys; all asserted actions use the
// public connector entry point and the normal JSON projection above.
func redisIdentityFixtureRoundTrip(t *testing.T, conn net.Conn, args []string, replies ...string) {
	t.Helper()
	if len(replies) == 0 {
		t.Fatal("fixture requires an exact expected reply")
	}
	var request strings.Builder
	fmt.Fprintf(&request, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&request, "$%d\r\n%s\r\n", len(arg), arg)
	}
	if written, err := io.WriteString(conn, request.String()); err != nil || written != request.Len() {
		t.Fatalf("fixture write failed: %v", err)
	}
	response := make([]byte, len(replies[0]))
	if _, err := io.ReadFull(conn, response); err != nil {
		t.Fatal(err)
	}
	for _, reply := range replies {
		if string(response) == reply {
			return
		}
	}
	t.Fatalf("fixture reply did not match: %q", response)
}
