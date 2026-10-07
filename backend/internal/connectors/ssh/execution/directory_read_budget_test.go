package execution

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func directoryFrame(kind byte, data []byte) []byte {
	packet := make([]byte, len(data)+9)
	binary.BigEndian.PutUint32(packet, uint32(len(data)+5))
	packet[4] = kind
	binary.BigEndian.PutUint32(packet[5:], 3)
	copy(packet[9:], data)
	return packet
}

type directoryPathFixture struct {
	resolved string
	entries  []os.FileInfo
	read     bool
}

func (c *directoryPathFixture) RealPath(string) (string, error) { return c.resolved, nil }
func (c *directoryPathFixture) ReadDir(string) ([]os.FileInfo, error) {
	c.read = true
	return c.entries, nil
}

func TestDirectoryPathsCannotAmplifyMetadata(t *testing.T) {
	client := &directoryPathFixture{resolved: "/" + strings.Repeat("x", 250<<10)}
	if items, err := listRemoteDirectoryWithClient(client, "."); err == nil || items != nil || client.read {
		t.Fatalf("oversized path reached ReadDir: %v %v", items, err)
	}
	file := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	client = &directoryPathFixture{resolved: "/" + strings.Repeat("x/", 2000), entries: make([]os.FileInfo, 2000)}
	for i := range client.entries {
		client.entries[i] = info
	}
	if items, err := listRemoteDirectoryWithClient(client, "."); err == nil || items != nil || !client.read {
		t.Fatalf("path multiplication escaped budget: %v %v", items, err)
	}
}

func TestDirectoryReadBudgetValidatesBeforeExposure(t *testing.T) {
	version := directoryFrame(2, nil)
	for _, size := range []int{1, 2, 4, 32} {
		guard := &directoryResponseBudget{source: bytes.NewReader(version)}
		var out bytes.Buffer
		buffer := make([]byte, size)
		for {
			n, err := guard.Read(buffer)
			out.Write(buffer[:n])
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(out.Bytes(), version) || guard.failure() != nil {
			t.Fatalf("size %d: %x", size, out.Bytes())
		}
	}
	count := make([]byte, 4)
	binary.BigEndian.PutUint32(count, maxDirectoryEntries+1)
	for name, input := range map[string][]byte{
		"too many entries":   directoryFrame(104, count),
		"missing attributes": directoryFrame(104, []byte{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}),
		"bad string length":  directoryFrame(102, []byte{0xff, 0xff, 0xff, 0xff}),
		"bad extensions":     directoryFrame(2, []byte{0, 0, 0, 1}),
		"unknown response":   directoryFrame(103, nil),
		"oversized packet":   {0, 4, 0, 1},
	} {
		t.Run(name, func(t *testing.T) {
			guard := &directoryResponseBudget{source: bytes.NewReader(input)}
			buffer := make([]byte, 8)
			n, err := guard.Read(buffer)
			if n != 0 || err == nil || guard.failure() == nil {
				t.Fatalf("exposed malformed data: n=%d err=%v", n, err)
			}
		})
	}
}

func TestDirectoryWireBudgetStopsBeforeReadingBody(t *testing.T) {
	input := bytes.NewReader(directoryFrame(2, nil))
	guard := &directoryResponseBudget{source: input, bytes: maxDirectoryWireBytes - 4}
	n, err := guard.Read(make([]byte, 4))
	if n != 0 || err == nil || !strings.Contains(err.Error(), "bounded read budget") || input.Len() != 5 {
		t.Fatalf("budget: n=%d err=%v remaining=%d", n, err, input.Len())
	}
}

func TestDirectoryAttributesAreBounded(t *testing.T) {
	for _, data := range [][]byte{{0, 0, 0, 0}, {0, 0, 0, 4, 0, 0, 1, 0}, {0x80, 0, 0, 0, 0, 0, 0, 0}} {
		if !skipDirectoryAttributes(bytes.NewReader(data)) {
			t.Fatalf("rejected valid attrs %x", data)
		}
	}
	for _, data := range [][]byte{{0, 0, 0, 1}, {0, 0, 0, 16}, {0x80, 0, 0, 0, 0xff, 0xff, 0xff, 0xff}} {
		if skipDirectoryAttributes(bytes.NewReader(data)) {
			t.Fatalf("accepted invalid attrs %x", data)
		}
	}
}
