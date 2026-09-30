package sessionenv

import (
	"bytes"
	"errors"
	"sync"
)

var redactedValue = []byte("[REDACTED VAULT VALUE]")

// Redactor replaces exact byte sequences while preserving matches split across
// arbitrary stream chunks.
type Redactor struct {
	mu       sync.Mutex
	patterns [][]byte
	pending  []byte
	closed   bool
}

func NewRedactor(patterns [][]byte) (*Redactor, error) {
	unique := map[string]bool{}
	values := make([][]byte, 0, len(patterns)*3)
	for _, pattern := range patterns {
		if len(pattern) == 0 {
			return nil, errors.New("redaction patterns cannot be empty")
		}
		variants := [][]byte{
			pattern,
			bytes.ReplaceAll(pattern, []byte("\n"), []byte("\r\n")),
			bytes.ReplaceAll(pattern, []byte("\n"), []byte("\r")),
		}
		for _, variant := range variants {
			key := string(variant)
			if unique[key] {
				continue
			}
			unique[key] = true
			values = append(values, bytes.Clone(variant))
		}
	}
	return &Redactor{patterns: values}, nil
}

func (r *Redactor) Write(chunk []byte) []byte {
	if r == nil || len(chunk) == 0 {
		return bytes.Clone(chunk)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	var output bytes.Buffer
	for _, value := range chunk {
		r.pending = append(r.pending, value)
		r.drain(&output, false)
	}
	return output.Bytes()
}

func (r *Redactor) Close() []byte {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	var output bytes.Buffer
	r.drain(&output, true)
	r.closed = true
	clear(r.pending)
	r.pending = nil
	destroyByteSlices(r.patterns)
	r.patterns = nil
	return output.Bytes()
}

// Redact applies the exact-value patterns to a complete value without changing
// the state of the streaming redactor.
func (r *Redactor) Redact(value []byte) []byte {
	if r == nil || len(value) == 0 {
		return bytes.Clone(value)
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return bytes.Clone(redactedValue)
	}
	patterns := make([][]byte, len(r.patterns))
	for index, pattern := range r.patterns {
		patterns[index] = bytes.Clone(pattern)
	}
	r.mu.Unlock()
	defer destroyByteSlices(patterns)

	current := value
	for range 3 {
		next := redactCompleteValue(current, patterns)
		if bytes.Equal(next, current) {
			return next
		}
		current = next
	}
	return bytes.Clone(redactedValue)
}

func redactCompleteValue(value []byte, patterns [][]byte) []byte {
	redactor := &Redactor{patterns: patterns}
	var output bytes.Buffer
	processed, searchAt := 0, 0
	for searchAt < len(value) {
		relative := bytes.Index(value[searchAt:], redactedValue)
		if relative < 0 {
			break
		}
		position := searchAt + relative
		searchAt = position + 1
		if markerOverlapsPattern(value, position, patterns) {
			continue
		}
		output.Write(redactor.Write(value[processed:position]))
		// Complete each segment with the same stream matcher. Only a whole
		// placeholder is exempt; actual secrets spanning its edges still match.
		redactor.drain(&output, true)
		output.Write(redactedValue)
		processed = position + len(redactedValue)
		searchAt = processed
	}
	output.Write(redactor.Write(value[processed:]))
	redactor.drain(&output, true)
	clear(redactor.pending[:cap(redactor.pending)])
	return output.Bytes()
}

func markerOverlapsPattern(value []byte, position int, patterns [][]byte) bool {
	markerEnd := position + len(redactedValue)
	for _, pattern := range patterns {
		if partialPatternCrossesMarker(value, position, pattern) {
			return true
		}
		start := max(0, position-len(pattern)+1)
		limit := min(len(value), markerEnd+len(pattern)-1)
		for start < markerEnd {
			relative := bytes.Index(value[start:limit], pattern)
			if relative < 0 {
				break
			}
			match := start + relative
			if match < position || match+len(pattern) > markerEnd {
				return true
			}
			start = match + 1
		}
	}
	return false
}

func partialPatternCrossesMarker(value []byte, position int, pattern []byte) bool {
	markerEnd := position + len(redactedValue)
	if len(value) <= markerEnd {
		return false
	}
	for start := position; start < markerEnd; start++ {
		if suffix := value[start:]; len(suffix) < len(pattern) && bytes.HasPrefix(pattern, suffix) {
			return true
		}
	}
	// A prefix starting before this marker must contain the entire marker.
	// Align only those occurrences rather than scanning every input suffix.
	for offset := 0; offset < len(pattern); {
		relative := bytes.Index(pattern[offset:], redactedValue)
		if relative < 0 {
			break
		}
		occurrence := offset + relative
		start := position - occurrence
		if start >= 0 && start < position {
			if suffix := value[start:]; len(suffix) < len(pattern) && bytes.HasPrefix(pattern, suffix) {
				return true
			}
		}
		offset = occurrence + 1
	}
	return false
}

func (r *Redactor) drain(output *bytes.Buffer, final bool) {
	for len(r.pending) > 0 {
		hasPotentialLonger := false
		exact := false
		for _, pattern := range r.patterns {
			if len(r.pending) <= len(pattern) && bytes.Equal(r.pending, pattern[:len(r.pending)]) {
				if len(r.pending) == len(pattern) {
					exact = true
				} else {
					hasPotentialLonger = true
				}
			}
		}
		if hasPotentialLonger {
			if !final {
				return
			}
			output.Write(redactedValue)
			r.pending = r.pending[:0]
			continue
		}
		if exact {
			output.Write(redactedValue)
			r.pending = r.pending[:0]
			continue
		}
		matchedPrefix := 0
		for _, pattern := range r.patterns {
			if len(pattern) <= len(r.pending) && bytes.Equal(pattern, r.pending[:len(pattern)]) && len(pattern) > matchedPrefix {
				matchedPrefix = len(pattern)
			}
		}
		if matchedPrefix > 0 {
			output.Write(redactedValue)
			r.pending = append(r.pending[:0], r.pending[matchedPrefix:]...)
			continue
		}
		output.WriteByte(r.pending[0])
		r.pending = append(r.pending[:0], r.pending[1:]...)
	}
}
