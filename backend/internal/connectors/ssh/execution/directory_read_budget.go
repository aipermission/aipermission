package execution

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	maxDirectoryWireBytes   = 4 << 20
	maxDirectoryEntries     = 5000
	maxDirectoryPacketBytes = 256 << 10
	maxDirectoryPathBytes   = 4096
)

// Bound directory accumulation before pkg/sftp receives any packet. Handshake,
// REALPATH names and dot entries count toward these per-browse limits.
type directoryResponseBudget struct {
	source  io.Reader
	pending []byte
	bytes   int
	entries uint32
	mu      sync.Mutex
	err     error
}

func (r *directoryResponseBudget) failure() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *directoryResponseBudget) reject(err error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err == nil {
		r.err = err
	}
	return r.err
}

func (r *directoryResponseBudget) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := r.failure(); err != nil {
		return 0, err
	}
	if len(r.pending) == 0 {
		var header [4]byte
		if _, err := io.ReadFull(r.source, header[:]); err != nil {
			return 0, err
		}
		size := binary.BigEndian.Uint32(header[:])
		if size < 5 || size > maxDirectoryPacketBytes || int(size)+4 > maxDirectoryWireBytes-r.bytes {
			return 0, r.reject(fmt.Errorf("SSH directory metadata exceeds the bounded read budget; choose a smaller directory"))
		}
		packet := make([]byte, int(size)+4)
		copy(packet, header[:])
		if _, err := io.ReadFull(r.source, packet[4:]); err != nil {
			return 0, err
		}
		if err := r.validate(packet[4:]); err != nil {
			return 0, r.reject(err)
		}
		r.bytes += len(packet)
		r.pending = packet
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}

func (r *directoryResponseBudget) validate(packet []byte) error {
	data := bytes.NewReader(packet[5:]) // packet type and request ID/version
	valid := false
	switch packet[0] {
	case 2: // VERSION: extension name/value pairs
		valid = true
		for data.Len() > 0 && valid {
			valid = skipDirectoryString(data) && skipDirectoryString(data)
		}
	case 102: // HANDLE
		valid = skipDirectoryString(data)
	case 101: // STATUS: code, message, language
		_, valid = directoryUint32(data)
		valid = valid && skipDirectoryString(data) && skipDirectoryString(data)
	case 104: // NAME: bounded entries, each with two strings and attributes
		count, ok := directoryUint32(data)
		if !ok {
			break
		}
		if count > maxDirectoryEntries-r.entries {
			return fmt.Errorf("SSH directory exceeds %d metadata entries; choose a smaller directory", maxDirectoryEntries)
		}
		r.entries += count
		valid = true
		for i := uint32(0); i < count && valid; i++ {
			valid = skipDirectoryString(data) && skipDirectoryString(data) && skipDirectoryAttributes(data)
		}
	}
	if !valid || data.Len() != 0 {
		return fmt.Errorf("SSH directory returned malformed SFTP metadata")
	}
	return nil
}

func directoryUint32(data *bytes.Reader) (uint32, bool) {
	var value uint32
	err := binary.Read(data, binary.BigEndian, &value)
	return value, err == nil
}

func skipDirectoryBytes(data *bytes.Reader, size uint64) bool {
	if size > uint64(data.Len()) {
		return false
	}
	_, err := data.Seek(int64(size), io.SeekCurrent)
	return err == nil
}

func skipDirectoryString(data *bytes.Reader) bool {
	size, ok := directoryUint32(data)
	return ok && skipDirectoryBytes(data, uint64(size))
}

func skipDirectoryAttributes(data *bytes.Reader) bool {
	flags, ok := directoryUint32(data)
	if !ok || flags & ^uint32(0x8000000f) != 0 {
		return false
	}
	for _, field := range []struct {
		flag  uint32
		bytes uint64
	}{{1, 8}, {2, 8}, {4, 4}, {8, 8}} {
		if flags&field.flag != 0 && !skipDirectoryBytes(data, field.bytes) {
			return false
		}
	}
	if flags&0x80000000 == 0 {
		return true
	}
	count, ok := directoryUint32(data)
	if !ok || uint64(count)*8 > uint64(data.Len()) {
		return false
	}
	for i := uint32(0); i < count; i++ {
		if !skipDirectoryString(data) || !skipDirectoryString(data) {
			return false
		}
	}
	return true
}

func newDirectorySFTPClient(connection *ssh.Client) (*sftp.Client, *directoryResponseBudget, error) {
	session, err := connection.NewSession()
	if err != nil {
		return nil, nil, err
	}
	writer, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		return nil, nil, err
	}
	reader, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		return nil, nil, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		_ = session.Close()
		return nil, nil, err
	}
	// RequestSubsystem does not start Session's usual stderr copy goroutine.
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	if err := session.RequestSubsystem("sftp"); err != nil {
		_ = session.Close()
		return nil, nil, err
	}
	budget := &directoryResponseBudget{source: reader}
	client, err := sftp.NewClientPipe(budget, directorySessionWriter{Writer: writer, session: session})
	if err != nil {
		_ = session.Close()
	}
	return client, budget, err
}

type directorySessionWriter struct {
	io.Writer
	session *ssh.Session
}

func (w directorySessionWriter) Close() error { return w.session.Close() }
