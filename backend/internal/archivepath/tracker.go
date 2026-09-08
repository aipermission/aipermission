// Package archivepath tracks portable file and directory identities in an archive hierarchy.
package archivepath

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type Tracker struct {
	used map[string]struct{}
}

func NewTracker() *Tracker {
	return &Tracker{used: map[string]struct{}{}}
}

func (t *Tracker) ConflictIndex(parts []string) int {
	for index := range parts {
		key := CanonicalKey(strings.Join(parts[:index+1], "/"))
		if index < len(parts)-1 {
			if t.has("file:" + key) {
				return index
			}
			continue
		}
		if t.has("file:"+key) || t.has("dir:"+key) {
			return index
		}
	}
	return -1
}

func (t *Tracker) Register(parts []string) {
	for index := range parts {
		kind := "dir:"
		if index == len(parts)-1 {
			kind = "file:"
		}
		t.used[kind+CanonicalKey(strings.Join(parts[:index+1], "/"))] = struct{}{}
	}
}

func (t *Tracker) has(key string) bool {
	_, ok := t.used[key]
	return ok
}

func CanonicalKey(value string) string {
	return cases.Fold().String(norm.NFC.String(strings.TrimRight(value, " .")))
}
