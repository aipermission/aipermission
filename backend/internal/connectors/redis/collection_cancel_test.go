package redisconnector

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type cancelPreviewRead struct {
	net.Conn
	cancel context.CancelFunc
	reads  int
}

func (conn *cancelPreviewRead) Read(buffer []byte) (int, error) {
	n, err := conn.Conn.Read(buffer)
	conn.reads++
	if conn.reads == 3 {
		conn.cancel()
	}
	return n, err
}

func TestCollectionCancellationAfterSuccessfulResponseCannotPublishSuccess(t *testing.T) {
	for _, kind := range []string{"hash", "set", "list", "zset"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			transport := scriptedTransport{t: t, handler: func(_ *testing.T, command []string) string {
				switch command[0] {
				case "TYPE":
					return "+" + kind + "\r\n"
				case "PTTL":
					return ":-1\r\n"
				}
				items := []string{"value"}
				if kind == "hash" {
					items = []string{"field", "value"}
				} else if kind == "zset" {
					items = append(items, "1")
				}
				if kind == "hash" || kind == "set" {
					return collectionScanReply("0", items)
				}
				return collectionArrayReply(items)
			}}
			conn, err := transport.DialConnectorTCP(ctx, connectors.NetworkDialRequest{})
			if err != nil {
				t.Fatal(err)
			}
			client := newRedisClient(&cancelPreviewRead{Conn: conn, cancel: cancel})
			client.bindContext(ctx)
			defer client.Close()
			result, err := executeGetKey(client, map[string]any{"key": "collection"})
			if !errors.Is(err, context.Canceled) || result.Status == connectors.ResultCompleted {
				t.Fatalf("completed response raced cancellation: result=%#v err=%v", result, err)
			}
		})
	}
}

func TestSortedSetPreviewRejectsMalformedScores(t *testing.T) {
	for _, score := range []string{"NaN", "invalid", ""} {
		preview := newCollectionPreview("zset", 100)
		if accepted, err := preview.appendEntry([]string{"member", score}); err == nil || accepted || preview.count != 0 {
			t.Fatalf("invalid score %q retained: accepted=%v count=%d err=%v", score, accepted, preview.count, err)
		}
	}
	for _, score := range []string{"-inf", "+inf", "1.25", "0"} {
		preview := newCollectionPreview("zset", 100)
		if accepted, err := preview.appendEntry([]string{"member", score}); err != nil || !accepted {
			t.Fatalf("valid score %q rejected: accepted=%v err=%v", score, accepted, err)
		}
	}
}
