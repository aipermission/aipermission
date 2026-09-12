package transport

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
)

type orderedCloser struct {
	name  string
	order *[]string
	err   error
}

func (closer orderedCloser) Close() error {
	*closer.order = append(*closer.order, closer.name)
	return closer.err
}

func TestCloseLiveConsoleProducerClosesClientBeforeSession(t *testing.T) {
	order := []string{}
	clientErr := errors.New("client close")
	sessionErr := errors.New("session close")
	err := closeLiveConsoleProducer(
		orderedCloser{name: "session", order: &order, err: sessionErr},
		orderedCloser{name: "client", order: &order, err: clientErr},
	)
	if len(order) != 2 || order[0] != "client" || order[1] != "session" {
		t.Fatalf("close order = %v, want client then session", order)
	}
	if !errors.Is(err, clientErr) || !errors.Is(err, sessionErr) {
		t.Fatalf("close error = %v, want both producer errors", err)
	}
}

type closeReleasedReader struct {
	started chan struct{}
	release chan struct{}
	exited  chan struct{}
	once    sync.Once
}

func (reader *closeReleasedReader) Read([]byte) (int, error) {
	reader.once.Do(func() { close(reader.started) })
	<-reader.release
	close(reader.exited)
	return 0, io.EOF
}

func TestLiveConsoleOutputCloseUnblocksAndJoinsReader(t *testing.T) {
	reader := &closeReleasedReader{
		started: make(chan struct{}), release: make(chan struct{}), exited: make(chan struct{}),
	}
	var closeCalls atomic.Int32
	output := newLiveConsoleOutput(func() error {
		<-reader.release
		return io.EOF
	}, func() error {
		if closeCalls.Add(1) == 1 {
			close(reader.release)
		}
		return nil
	})
	if !output.Start(reader, nil) {
		t.Fatal("output owner refused its first reader")
	}
	<-reader.started
	closed := make(chan error, 1)
	go func() { closed <- output.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not join the blocked reader")
	}
	select {
	case <-reader.exited:
	default:
		t.Fatal("Close returned before the reader exited")
	}
	if closeCalls.Load() != 1 {
		t.Fatalf("producer close calls = %d, want 1", closeCalls.Load())
	}
}

type bufferInspectionReader struct {
	secret  []byte
	reads   int
	cleared bool
}

func (reader *bufferInspectionReader) Read(buffer []byte) (int, error) {
	reader.reads++
	if reader.reads == 1 {
		return copy(buffer, reader.secret), nil
	}
	reader.cleared = bytes.Equal(buffer[:len(reader.secret)], make([]byte, len(reader.secret)))
	return 0, io.EOF
}

func TestLiveConsoleOutputClearsReusablePlaintextBuffer(t *testing.T) {
	reader := &bufferInspectionReader{secret: []byte("temporary-secret-value")}
	destination := make(chan console.RuntimeOutput, 1)
	buffer := make([]byte, liveConsoleOutputBufferSize)
	pumpLiveConsoleOutput(t.Context(), reader, console.RuntimeStdout, destination, buffer)
	if !reader.cleared {
		t.Fatal("output pump retained plaintext before the next read")
	}
	if !bytes.Equal(buffer, make([]byte, len(buffer))) {
		t.Fatal("output pump retained plaintext after returning")
	}
}

func TestLiveConsoleOutputStartCloseRaceOwnsExactlyOneLifecycle(t *testing.T) {
	for range 100 {
		release := make(chan struct{})
		var releaseOnce sync.Once
		var closeCalls atomic.Int32
		output := newLiveConsoleOutput(func() error {
			<-release
			return nil
		}, func() error {
			closeCalls.Add(1)
			releaseOnce.Do(func() { close(release) })
			return nil
		})
		var callers sync.WaitGroup
		callers.Add(2)
		go func() { defer callers.Done(); output.Start(bytes.NewReader(nil), nil) }()
		go func() { defer callers.Done(); _ = output.Close() }()
		callers.Wait()
		if closeCalls.Load() != 1 {
			t.Fatalf("producer close calls = %d, want 1", closeCalls.Load())
		}
	}
}
