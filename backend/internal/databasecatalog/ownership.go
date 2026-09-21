package databasecatalog

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/aipermission/aipermission/backend/internal/databaseownership"
)

func acquireDatabaseOwnershipSet(paths ...string) ([]*databaseownership.Ownership, error) {
	unique := make(map[string]struct{}, len(paths))
	ordered := make([]string, 0, len(paths))
	for _, path := range paths {
		absolutePath, err := filepath.Abs(filepath.Clean(path))
		if err != nil {
			return nil, fmt.Errorf("resolve database ownership set: %w", err)
		}
		if _, exists := unique[absolutePath]; exists {
			continue
		}
		unique[absolutePath] = struct{}{}
		ordered = append(ordered, absolutePath)
	}
	sort.Strings(ordered)
	ownerships := make([]*databaseownership.Ownership, 0, len(ordered))
	for _, path := range ordered {
		ownership, err := databaseownership.Acquire(path)
		if err != nil {
			closeDatabaseOwnershipSet(ownerships)
			return nil, err
		}
		ownerships = append(ownerships, ownership)
	}
	return ownerships, nil
}

func closeDatabaseOwnershipSet(ownerships []*databaseownership.Ownership) {
	for index := len(ownerships) - 1; index >= 0; index-- {
		_ = ownerships[index].Close()
	}
}
