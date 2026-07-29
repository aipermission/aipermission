// Package sessionenv owns in-memory secret envelopes and exact-value output
// redaction for connector sessions. Envelopes deliberately have no JSON or
// text representation.
package sessionenv

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	MaxItems      = 64
	MaxValueBytes = 16 * 1024
	MaxTotalBytes = 256 * 1024
)

var ErrDestroyed = errors.New("secret environment envelope is destroyed")

type EntryInput struct {
	Name            string
	Value           []byte
	ReplaceExisting bool
	ItemID          int64
	ValueVersion    int64
	SourceProjectID int64
}

// EntryView exists only during Envelope.WithEntries. Callers must not retain
// Value after the callback returns.
type EntryView struct {
	Name            string
	Value           []byte
	ReplaceExisting bool
	ItemID          int64
	ValueVersion    int64
	SourceProjectID int64
}

type entry struct {
	name            string
	value           []byte
	replaceExisting bool
	itemID          int64
	valueVersion    int64
	sourceProjectID int64
}

type Envelope struct {
	mu        sync.Mutex
	entries   []entry
	destroyed bool
}

func NewEnvelope(inputs []EntryInput) (*Envelope, error) {
	if len(inputs) == 0 {
		return &Envelope{}, nil
	}
	if len(inputs) > MaxItems {
		return nil, fmt.Errorf("secret environment supports at most %d items", MaxItems)
	}
	seen := map[string]bool{}
	total := 0
	entries := make([]entry, 0, len(inputs))
	for _, input := range inputs {
		if err := ValidateName(input.Name); err != nil {
			destroyEntries(entries)
			return nil, err
		}
		if seen[input.Name] {
			destroyEntries(entries)
			return nil, fmt.Errorf("duplicate environment name %q", input.Name)
		}
		seen[input.Name] = true
		if len(input.Value) == 0 {
			destroyEntries(entries)
			return nil, fmt.Errorf("environment value for %s is empty", input.Name)
		}
		if len(input.Value) > MaxValueBytes {
			destroyEntries(entries)
			return nil, fmt.Errorf("environment value for %s exceeds %d bytes", input.Name, MaxValueBytes)
		}
		if bytes.IndexByte(input.Value, 0) >= 0 {
			destroyEntries(entries)
			return nil, fmt.Errorf("environment value for %s contains a NUL byte", input.Name)
		}
		total += len(input.Value)
		if total > MaxTotalBytes {
			destroyEntries(entries)
			return nil, fmt.Errorf("secret environment exceeds %d bytes", MaxTotalBytes)
		}
		entries = append(entries, entry{
			name: input.Name, value: bytes.Clone(input.Value),
			replaceExisting: input.ReplaceExisting, itemID: input.ItemID,
			valueVersion: input.ValueVersion, sourceProjectID: input.SourceProjectID,
		})
	}
	return &Envelope{entries: entries}, nil
}

func (e *Envelope) Len() int {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.destroyed {
		return 0
	}
	return len(e.entries)
}

func (e *Envelope) Names() []string {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.destroyed {
		return nil
	}
	names := make([]string, 0, len(e.entries))
	for _, item := range e.entries {
		names = append(names, item.name)
	}
	return names
}

func (e *Envelope) WithEntries(fn func([]EntryView) error) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.destroyed {
		return ErrDestroyed
	}
	views := make([]EntryView, 0, len(e.entries))
	for _, item := range e.entries {
		views = append(views, EntryView{
			Name: item.name, Value: item.value, ReplaceExisting: item.replaceExisting,
			ItemID: item.itemID, ValueVersion: item.valueVersion,
			SourceProjectID: item.sourceProjectID,
		})
	}
	defer func() {
		for index := range views {
			views[index].Value = nil
		}
	}()
	return fn(views)
}

func (e *Envelope) ExactValueRedactor() (*Redactor, error) {
	patterns := [][]byte{}
	err := e.WithEntries(func(entries []EntryView) error {
		for _, item := range entries {
			patterns = append(patterns, bytes.Clone(item.Value))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	redactor, err := NewRedactor(patterns)
	destroyByteSlices(patterns)
	return redactor, err
}

func (e *Envelope) Destroy() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.destroyed {
		return
	}
	destroyEntries(e.entries)
	e.entries = nil
	e.destroyed = true
}

func ValidateName(value string) error {
	if value == "" {
		return errors.New("environment name is required")
	}
	if utf8.RuneCountInString(value) > 128 {
		return errors.New("environment name is too long")
	}
	for index, r := range value {
		if (r >= 'A' && r <= 'Z') || r == '_' || (index > 0 && r >= '0' && r <= '9') {
			continue
		}
		return errors.New("environment name must use uppercase letters, digits, and underscores, and cannot start with a digit")
	}
	if ReservedName(value) {
		return errors.New("environment name is reserved and cannot be injected")
	}
	return nil
}

func ReservedName(value string) bool {
	switch value {
	case "PATH", "HOME", "SHELL", "USER", "LOGNAME", "IFS", "ENV", "BASH_ENV",
		"CDPATH", "PROMPT_COMMAND", "HISTFILE", "GCONV_PATH", "OPENSSL_CONF",
		"PYTHONHOME", "PYTHONPATH", "RUBYOPT", "PERL5OPT", "NODE_OPTIONS",
		"GIT_CONFIG", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_GLOBAL", "GIT_SSH",
		"GIT_SSH_COMMAND", "SSH_AUTH_SOCK", "SSH_ASKPASS":
		return true
	}
	for _, prefix := range []string{
		"LD_", "DYLD_", "BASH_FUNC_", "GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_",
		"PYTHONINSPECT", "PERL5LIB",
	} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func destroyEntries(entries []entry) {
	for index := range entries {
		clear(entries[index].value)
		entries[index].value = nil
	}
}

func destroyByteSlices(values [][]byte) {
	for index := range values {
		clear(values[index])
		values[index] = nil
	}
}
