package mssql

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-sql/sqlexp"
	"github.com/microsoft/go-mssqldb/msdsn"
)

type closeProbeTransport struct {
	io.Reader
	closed chan struct{}
	once   sync.Once
}

func (p *closeProbeTransport) Write(b []byte) (int, error) { return len(b), nil }
func (p *closeProbeTransport) Close() error                { p.once.Do(func() { close(p.closed) }); return nil }

type closeProbeLogger struct{ entered chan struct{} }

func (p closeProbeLogger) Log(_ context.Context, category msdsn.Log, _ string) {
	if category == msdsn.LogErrors {
		close(p.entered)
	}
}

func TestCloseJoinsReaderBeforeReturningBufferAndIsIdempotent(t *testing.T) {
	transport := &closeProbeTransport{Reader: bytes.NewReader(nil), closed: make(chan struct{})}
	done := make(chan struct{})
	var returned atomic.Int32
	var aborted atomic.Bool
	conn := &Conn{sess: &tdsSession{readDone: done, readAbort: func() { aborted.Store(true) },
		buf: &tdsBuffer{transport: transport, bufClose: func() { returned.Add(1) }}}}
	finished := make(chan struct{})
	go func() { _ = conn.Close(); close(finished) }()
	select {
	case <-transport.closed:
	case <-time.After(time.Second):
		t.Fatal("transport did not close")
	}
	if !aborted.Load() || returned.Load() != 0 {
		t.Error("reader was not aborted before buffer release")
	}
	select {
	case <-finished:
		t.Error("Close returned before reader exit")
	default:
	}
	close(done)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Close did not join reader")
	}
	var callers sync.WaitGroup
	for range 8 {
		callers.Go(func() { _ = conn.Close() })
	}
	callers.Wait()
	if returned.Load() != 1 {
		t.Fatalf("buffer returned %d times", returned.Load())
	}
}

func TestCloseUnblocksFullTokenAndMessageQueues(t *testing.T) {
	for _, messages := range []bool{false, true} {
		t.Run(map[bool]string{false: "tokens", true: "messages"}[messages], func(t *testing.T) {
			payload := make([]byte, 0, 100)
			for range 20 {
				payload = append(payload, 0x79, 0, 0, 0, 0)
			}
			packet := make([]byte, 8)
			packet[0], packet[1] = byte(packReply), 1
			binary.BigEndian.PutUint16(packet[2:4], uint16(8+len(payload)))
			packet = append(packet, payload...)
			transport := &closeProbeTransport{Reader: bytes.NewReader(packet), closed: make(chan struct{})}
			session := &tdsSession{buf: newTdsBuffer(4096, transport)}
			messageAttempt := make(chan struct{})
			var outs outputs
			if messages {
				session.logger = closeProbeLogger{entered: messageAttempt}
				session.logFlags = uint64(msdsn.LogErrors)
				outs.msgq = &sqlexp.ReturnMessage{}
				sqlexp.ReturnMessageInit(outs.msgq)
				for range 15 {
					if err := sqlexp.ReturnMessageEnqueue(t.Context(), outs.msgq, sqlexp.MsgNext{}); err != nil {
						t.Fatal(err)
					}
				}
				// An invalid packet forces the reader's panic/error message path.
				packet[0] = 0
			}
			tokens := make(chan tokenStruct, 1)
			session.startResponseReader(context.Background(), tokens, outs)
			if messages {
				select {
				case <-messageAttempt:
				case <-time.After(time.Second):
					t.Fatal("reader did not reach its full message queue")
				}
			} else {
				deadline := time.Now().Add(time.Second)
				for len(tokens) != cap(tokens) && time.Now().Before(deadline) {
					runtime.Gosched()
				}
				if len(tokens) != cap(tokens) {
					t.Fatal("reader did not fill its token queue")
				}
			}
			conn := &Conn{sess: session}
			closed := make(chan struct{})
			go func() { _ = conn.Close(); close(closed) }()
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("queue backpressure prevented shutdown")
			}
			select {
			case <-session.readDone:
			default:
				t.Fatal("reader still running after Close")
			}
		})
	}
}
