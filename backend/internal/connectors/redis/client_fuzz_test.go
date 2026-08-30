package redisconnector

import (
	"bufio"
	"bytes"
	"testing"
)

func FuzzReadRESPValue(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("+OK\r\n"),
		[]byte(":42\r\n"),
		[]byte("$5\r\nhello\r\n"),
		[]byte("*2\r\n$3\r\nGET\r\n$3\r\nkey\r\n"),
		[]byte("$999999999\r\n"),
		[]byte("*999999999\r\n"),
		[]byte("*1\r\n*1\r\n*1\r\n"),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 2<<20 {
			return
		}
		value, err := readRESPValue(bufio.NewReader(bytes.NewReader(payload)))
		if err != nil {
			return
		}
		assertRESPValueBounds(t, value, 0)
	})
}

func assertRESPValueBounds(t *testing.T, value respValue, depth int) int {
	t.Helper()
	if depth > maxRESPNestingDepth {
		t.Fatalf("parsed RESP value exceeds nesting limit: %d", depth)
	}
	if len(value.text) > maxRESPBulkBytes {
		t.Fatalf("parsed RESP value exceeds bulk limit: %d", len(value.text))
	}
	if len(value.array) > maxRESPArrayItems {
		t.Fatalf("parsed RESP array exceeds item limit: %d", len(value.array))
	}
	count := 1
	for _, item := range value.array {
		count += assertRESPValueBounds(t, item, depth+1)
	}
	if count > maxRESPValues {
		t.Fatalf("parsed RESP value exceeds value budget: %d", count)
	}
	return count
}
