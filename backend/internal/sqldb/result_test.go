package sqldb

import (
	"errors"
	"strings"
	"testing"
)

type resultStub struct {
	affected int64
	err      error
}

func (resultStub) LastInsertId() (int64, error)   { return 0, nil }
func (r resultStub) RowsAffected() (int64, error) { return r.affected, r.err }

func TestRowsAffectedReturnsDriverCount(t *testing.T) {
	affected, err := RowsAffected(resultStub{affected: 3}, "update records")
	if err != nil || affected != 3 {
		t.Fatalf("RowsAffected() = %d, %v", affected, err)
	}
}

func TestRowsAffectedWrapsDriverError(t *testing.T) {
	driverErr := errors.New("count unavailable")
	_, err := RowsAffected(resultStub{err: driverErr}, "update records")
	if !errors.Is(err, driverErr) || !strings.Contains(err.Error(), "update records") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRowsAffectedRejectsNilResult(t *testing.T) {
	if _, err := RowsAffected(nil, "update records"); err == nil {
		t.Fatal("expected nil result error")
	}
}
