package sessionenvprotocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

func TestMetadataNeverContainsValues(t *testing.T) {
	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
		Name: "MY_PROJECT_API_KEY", Value: []byte("value with spaces\nand unicode: merhaba"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer envelope.Destroy()
	metadata, total, err := metadataFrames(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if total == 0 || strings.Contains(metadata, "value with spaces") {
		t.Fatalf("unsafe metadata: %q", metadata)
	}
}

func TestBootstrapCancellationInterruptsBlockedWrites(t *testing.T) {
	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{Name: "TOKEN", Value: []byte("secret-value")}})
	if err != nil {
		t.Fatal(err)
	}
	defer envelope.Destroy()
	protocol, err := New(1)
	if err != nil {
		t.Fatal(err)
	}
	writer := newBlockingBootstrapWriter()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := protocol.Bootstrap(ctx, writer, strings.NewReader(""), envelope)
		done <- err
	}()
	<-writer.started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("bootstrap error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt the blocked bootstrap write")
	}
	if !writer.wasClosed() {
		t.Fatal("bootstrap input was not closed on cancellation")
	}
}

func TestBootstrapCancellationDuringValueWriteReturnsContextError(t *testing.T) {
	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{Name: "TOKEN", Value: []byte("secret-value")}})
	if err != nil {
		t.Fatal(err)
	}
	defer envelope.Destroy()
	protocol, err := New(1)
	if err != nil {
		t.Fatal(err)
	}
	writer := newValueBlockingBootstrapWriter()
	stdout := strings.NewReader("READY " + Version + " " + protocol.nonce + " 1\n")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := protocol.Bootstrap(ctx, writer, stdout, envelope)
		done <- err
	}()
	<-writer.started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("bootstrap error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt the blocked value write")
	}
}

type blockingBootstrapWriter struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

type valueBlockingBootstrapWriter struct {
	started chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func newValueBlockingBootstrapWriter() *valueBlockingBootstrapWriter {
	return &valueBlockingBootstrapWriter{started: make(chan struct{}), closed: make(chan struct{})}
}

func (w *valueBlockingBootstrapWriter) Write(value []byte) (int, error) {
	if !bytes.HasPrefix(value, []byte("VALUE ")) {
		return len(value), nil
	}
	w.once.Do(func() { close(w.started) })
	<-w.closed
	return 0, io.ErrClosedPipe
}

func (w *valueBlockingBootstrapWriter) Close() error {
	select {
	case <-w.closed:
	default:
		close(w.closed)
	}
	return nil
}

func newBlockingBootstrapWriter() *blockingBootstrapWriter {
	return &blockingBootstrapWriter{started: make(chan struct{}), closed: make(chan struct{})}
}

func (w *blockingBootstrapWriter) Write([]byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.closed
	return 0, io.ErrClosedPipe
}

func (w *blockingBootstrapWriter) Close() error {
	select {
	case <-w.closed:
	default:
		close(w.closed)
	}
	return nil
}

func (w *blockingBootstrapWriter) wasClosed() bool {
	select {
	case <-w.closed:
		return true
	default:
		return false
	}
}

func TestBootstrapUsesMetadataHandshakeBeforeValues(t *testing.T) {
	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
		Name: "MY_PROJECT_API_KEY", Value: []byte("secret-value"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer envelope.Destroy()

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdinReader.Close()
	defer stdoutWriter.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type bootstrapResult struct {
		result Result
		err    error
	}
	done := make(chan bootstrapResult, 1)
	protocol, err := New(7)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		result, err := protocol.Bootstrap(ctx, stdinWriter, stdoutReader, envelope)
		done <- bootstrapResult{result: result, err: err}
	}()

	reader := bufio.NewReader(stdinReader)
	var received bytes.Buffer
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		received.WriteString(line)
		if strings.HasPrefix(line, "META_END ") {
			break
		}
	}
	if strings.Contains(received.String(), "secret-value") || strings.Contains(received.String(), "c2VjcmV0LXZhbHVl") {
		t.Fatal("value frame was sent before metadata acknowledgement")
	}
	headerLine := findLine(received.String(), Version+" ")
	parts := strings.Fields(headerLine)
	if len(parts) != 5 {
		t.Fatalf("invalid header: %q", headerLine)
	}
	_, _ = io.WriteString(stdoutWriter, "login banner\nroot@host:~# READY "+Version+" "+parts[1]+" 7\n")
	valueLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(valueLine, "VALUE 1 ") || strings.Contains(valueLine, "secret-value") {
		t.Fatalf("invalid value frame: %q", valueLine)
	}
	endLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(endLine, "END ") {
		t.Fatalf("invalid end frame: %q", endLine)
	}
	_, _ = io.WriteString(stdoutWriter, "ACK "+Version+" "+parts[1]+" 7\n")
	bootstrap := <-done
	if bootstrap.err != nil {
		t.Fatalf("bootstrap: %v", bootstrap.err)
	}
	if prelude := string(bootstrap.result.Prelude); !strings.Contains(prelude, "login banner") || !strings.Contains(prelude, "root@host:~# ") {
		t.Fatalf("bootstrap prelude = %q", prelude)
	}
}

func TestBootstrapPreservesTextBeforeReadyFrameOnSameLine(t *testing.T) {
	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
		Name: "MY_PROJECT_API_KEY", Value: []byte("secret-value"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer envelope.Destroy()

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdinReader.Close()
	defer stdoutWriter.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	protocol, err := New(11)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan bootstrapResultForTest, 1)
	go func() {
		result, err := protocol.Bootstrap(ctx, stdinWriter, stdoutReader, envelope)
		done <- bootstrapResultForTest{result: result, err: err}
	}()

	reader := bufio.NewReader(stdinReader)
	header, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Fields(header)
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.HasPrefix(line, "META_END ") {
			break
		}
	}
	_, _ = io.WriteString(stdoutWriter, "banner without newline READY "+Version+" "+parts[1]+" 11\n")
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(stdoutWriter, "ACK "+Version+" "+parts[1]+" 11\n")
	bootstrap := <-done
	if bootstrap.err != nil {
		t.Fatal(bootstrap.err)
	}
	if got := string(bootstrap.result.Prelude); got != "banner without newline " {
		t.Fatalf("prelude = %q", got)
	}
}

type bootstrapResultForTest struct {
	result Result
	err    error
}

func TestBootstrapFiltersSingleLineWrapperEcho(t *testing.T) {
	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
		Name: "MY_PROJECT_API_KEY", Value: []byte("secret-value"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer envelope.Destroy()

	protocol, err := New(7)
	if err != nil {
		t.Fatal(err)
	}
	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	defer stdinReader.Close()
	defer stdoutWriter.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		result, err := protocol.Bootstrap(ctx, stdinWriter, stdoutReader, envelope)
		if err == nil && strings.Contains(string(result.Prelude), protocol.Command()) {
			err = errors.New("wrapper command leaked into the visible prelude")
		}
		done <- err
	}()

	reader := bufio.NewReader(stdinReader)
	header, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Fields(header)
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.HasPrefix(line, "META_END ") {
			break
		}
	}
	_, _ = io.WriteString(stdoutWriter, "login banner\nroot@host:~# "+protocol.Command()+"\n")
	_, _ = io.WriteString(stdoutWriter, "READY "+Version+" "+parts[1]+" 7\n")
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(stdoutWriter, "ACK "+Version+" "+parts[1]+" 7\n")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWaitForFrameEnforcesConsumedByteLimit(t *testing.T) {
	expected := "READY APENV/1 fixture 1"
	for _, testCase := range []struct {
		name      string
		input     string
		wantError bool
		wantBytes int
	}{
		{
			name:      "fragmented frame at limit",
			input:     strings.Repeat("x", maxPreludeBytes-len(expected)-1) + expected + "\n",
			wantBytes: maxPreludeBytes - len(expected) - 1,
		},
		{
			name:      "frame one byte over limit",
			input:     strings.Repeat("x", maxPreludeBytes-len(expected)) + expected + "\n",
			wantError: true,
		},
		{
			name:      "newline free input over limit",
			input:     strings.Repeat("x", maxPreludeBytes+1),
			wantError: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			prelude, err := waitForFrame(t.Context(), bufio.NewReader(strings.NewReader(testCase.input)), expected)
			if (err != nil) != testCase.wantError {
				t.Fatalf("err = %v, wantError %t", err, testCase.wantError)
			}
			if len(prelude) != testCase.wantBytes {
				t.Fatalf("prelude bytes = %d, want %d", len(prelude), testCase.wantBytes)
			}
		})
	}
}

func TestWaitForFrameCountsIgnoredLines(t *testing.T) {
	expected := "READY APENV/1 fixture 1"
	ignored := "wrapper-command"
	line := "prompt " + ignored + "\n"
	input := strings.Repeat(line, maxPreludeBytes/len(line)+1) + expected + "\n"
	if _, err := waitForFrame(t.Context(), bufio.NewReader(strings.NewReader(input)), expected, ignored); err == nil || !strings.Contains(err.Error(), "safety limit") {
		t.Fatalf("ignored-line overflow error = %v", err)
	}
}

func TestPOSIXBootstrapPreservesComplexValues(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("POSIX shell is unavailable")
	}
	if _, err := exec.LookPath("base64"); err != nil {
		t.Skip("base64 is unavailable")
	}
	value := []byte(" spaces ' quotes \" unicode merhaba\ntrailing\n")
	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
		Name: "MY_PROJECT_COMPLEX_VALUE", Value: value,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer envelope.Destroy()
	const nonce = "00112233445566778899aabbccddeeff"
	const generation = int64(9)
	protocol := &Protocol{nonce: nonce, generation: generation}
	metadata, total, err := metadataFrames(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	_, _ = io.WriteString(&input, Version+" "+nonce+" "+strconv.FormatInt(generation, 10)+" 1 "+strconv.Itoa(total)+"\n")
	input.WriteString(metadata)
	input.WriteString("META_END " + nonce + "\n")
	if err := writeValueFrames(t.Context(), &input, envelope, nonce); err != nil {
		t.Fatal(err)
	}
	input.WriteString(`printf 'VALUE_B64='; printf '%s' "$MY_PROJECT_COMPLEX_VALUE" | base64 | tr -d '\n'; printf '\n'` + "\n")

	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "stty"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "-c", compactShellScript(bootstrapScript(nonce, generation)))
	command.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	inputText := input.String()
	command.Stdin = strings.NewReader(inputText)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("execute POSIX bootstrap: %v\n%s\n--- input ---\n%s", err, output, inputText)
	}
	expected := "VALUE_B64=" + base64.StdEncoding.EncodeToString(value)
	if !strings.Contains(string(output), "READY "+Version+" "+nonce+" 9") ||
		!strings.Contains(string(output), "ACK "+Version+" "+nonce+" 9") ||
		!strings.Contains(string(output), expected) {
		t.Fatalf("unexpected bootstrap output:\n%s", output)
	}
	if strings.Contains(protocol.Command(), string(value)) || strings.Contains(protocol.Command(), base64.StdEncoding.EncodeToString(value)) {
		t.Fatal("wrapper command contains a secret value")
	}
	if !strings.Contains(protocol.Command(), "__apenv_user_shell=${SHELL:-/bin/sh}") ||
		!strings.Contains(bootstrapScript(nonce, generation), `exec "$__apenv_shell" -i`) {
		t.Fatal("bootstrap does not preserve the account shell")
	}
	if strings.Contains(protocol.Command(), "\n") {
		t.Fatal("wrapper command must not trigger interactive continuation prompts")
	}
	if len(protocol.Command()) >= 4096 {
		t.Fatalf("wrapper command exceeds the canonical terminal input limit: %d bytes", len(protocol.Command()))
	}
	compactScript := compactShellScript(bootstrapScript(nonce, generation))
	check := exec.Command("sh", "-n")
	check.Stdin = strings.NewReader(compactScript)
	if output, err := check.CombinedOutput(); err != nil {
		t.Fatalf("compact bootstrap syntax: %v\n%s\n%s", err, output, compactScript)
	}
}

func TestPOSIXBootstrapRejectsInvalidBase64ValueFrame(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("POSIX shell is unavailable")
	}
	if _, err := exec.LookPath("base64"); err != nil {
		t.Skip("base64 is unavailable")
	}
	envelope, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{
		Name: "MY_PROJECT_VALUE", Value: []byte("secret-value"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer envelope.Destroy()
	const nonce = "00112233445566778899aabbccddeeff"
	const generation = int64(9)
	metadata, total, err := metadataFrames(envelope)
	if err != nil {
		t.Fatal(err)
	}
	encodedLength := base64.StdEncoding.EncodedLen(len("secret-value"))
	var input bytes.Buffer
	_, _ = io.WriteString(&input, Version+" "+nonce+" "+strconv.FormatInt(generation, 10)+" 1 "+strconv.Itoa(total)+"\n")
	input.WriteString(metadata)
	input.WriteString("META_END " + nonce + "\n")
	input.WriteString("VALUE 1 " + strings.Repeat("!", encodedLength) + "\n")
	input.WriteString("END " + nonce + "\n")

	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "stty"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "-c", compactShellScript(bootstrapScript(nonce, generation)))
	command.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
	command.Stdin = strings.NewReader(input.String())
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("invalid base64 frame succeeded:\n%s", output)
	}
	if strings.Contains(string(output), "ACK "+Version) {
		t.Fatalf("invalid base64 frame was acknowledged:\n%s", output)
	}
}

func findLine(value, prefix string) string {
	for _, line := range strings.Split(value, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}
