package serviceboundary

import (
	"bytes"
	"io"
)

// ScanningWriter withholds a suffix long enough to validate credentials split
// across chunks. Use Flush only after a successful complete copy.
type ScanningWriter struct {
	destination io.Writer
	patterns    [][]byte
	tail        []byte
	maxPattern  int
	err         error
}

func (writer *ScanningWriter) Write(payload []byte) (int, error) {
	if err := writer.failure(); err != nil {
		return 0, err
	}
	window := make([]byte, 0, len(writer.tail)+len(payload))
	window = append(window, writer.tail...)
	window = append(window, payload...)
	for _, pattern := range writer.patterns {
		if bytes.Contains(window, pattern) {
			writer.err = ErrReflectedCredential
			return 0, ErrReflectedCredential
		}
	}
	keep := writer.maxPattern - 1
	if keep > len(window) {
		keep = len(window)
	}
	safe := window[:len(window)-keep]
	if len(safe) > 0 {
		if err := writer.writeVerified(safe); err != nil {
			return 0, err
		}
	}
	writer.tail = append(writer.tail[:0], window[len(window)-keep:]...)
	return len(payload), nil
}

func (writer *ScanningWriter) Flush() error {
	if err := writer.failure(); err != nil {
		return err
	}
	if len(writer.tail) == 0 {
		return nil
	}
	if err := writer.writeVerified(writer.tail); err != nil {
		return err
	}
	writer.tail = nil
	return nil
}

func (writer *ScanningWriter) failure() error {
	if writer == nil || writer.destination == nil || len(writer.patterns) == 0 || writer.maxPattern < 1 {
		return ErrUnavailable
	}
	return writer.err
}

func (writer *ScanningWriter) writeVerified(payload []byte) error {
	written, err := writer.destination.Write(payload)
	if err == nil && written != len(payload) {
		err = io.ErrShortWrite
	}
	writer.err = err
	return err
}
