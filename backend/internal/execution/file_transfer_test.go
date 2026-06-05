package execution

import (
	"bytes"
	"io"
	"testing"
)

func TestProgressReaderReportsTransferredBytes(t *testing.T) {
	var seenTransferred int64
	var seenTotal int64
	reader := &progressReader{
		reader: bytes.NewBufferString("hello"),
		total:  5,
		fn: func(transferred int64, total int64) {
			seenTransferred = transferred
			seenTotal = total
		},
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read progress reader: %v", err)
	}
	if string(data) != "hello" || seenTransferred != 5 || seenTotal != 5 {
		t.Fatalf("unexpected progress reader state data=%q transferred=%d total=%d", data, seenTransferred, seenTotal)
	}
}

func TestProgressWriterReportsTransferredBytes(t *testing.T) {
	var output bytes.Buffer
	var seenTransferred int64
	var seenTotal int64
	writer := &progressWriter{
		writer: &output,
		total:  5,
		fn: func(transferred int64, total int64) {
			seenTransferred = transferred
			seenTotal = total
		},
	}
	n, err := writer.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("write progress writer: %v", err)
	}
	if n != 5 || output.String() != "hello" || seenTransferred != 5 || seenTotal != 5 {
		t.Fatalf("unexpected progress writer state n=%d output=%q transferred=%d total=%d", n, output.String(), seenTransferred, seenTotal)
	}
}
