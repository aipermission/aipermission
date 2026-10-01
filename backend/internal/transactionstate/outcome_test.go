package transactionstate

import (
	"errors"
	"fmt"
	"testing"
)

func TestTransactionFinalityPreservesCauseAndFailsClosed(t *testing.T) {
	cause := errors.New("driver failed")
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unclassified", cause, false},
		{"confirmed", NotCommitted(cause), true},
		{"wrapped confirmed", fmt.Errorf("context: %w", NotCommitted(cause)), true},
		{"unknown", Unknown(cause), false},
		{"safe readback unknown", UnknownWithSafeReadback(cause), false},
		{"unknown masks nested marker", Unknown(NotCommitted(cause)), false},
		{"unknown joined causes", Unknown(errors.Join(NotCommitted(cause), cause)), false},
		{"joined contradictory finality", errors.Join(NotCommitted(cause), Unknown(cause)), false},
		{"joined unclassified branch", errors.Join(NotCommitted(cause), cause), false},
		{"joined confirmed branches", errors.Join(NotCommitted(cause), NotCommitted(cause)), true},
		{"typed nil marker", (*failure)(nil), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsNotCommitted(test.err); got != test.want {
				t.Fatalf("not committed = %v, want %v", got, test.want)
			}
			if test.name != "typed nil marker" && test.err != nil && (!errors.Is(test.err, cause) || test.err.Error() == "") {
				t.Fatalf("transaction finality lost cause: %v", test.err)
			}
		})
	}
	if NotCommitted(nil) != nil || Unknown(nil) != nil || UnknownWithSafeReadback(nil) != nil {
		t.Fatal("nil outcome became a transaction failure")
	}
}

func TestTransactionReadbackRequiresExplicitOwnerFinality(t *testing.T) {
	cause := errors.New("driver failed")
	for _, test := range []struct {
		err  error
		want bool
	}{
		{nil, false}, {cause, false}, {NotCommitted(cause), false}, {Unknown(cause), false},
		{UnknownWithSafeReadback(cause), true},
		{fmt.Errorf("owner: %w", UnknownWithSafeReadback(cause)), true},
		{Unknown(UnknownWithSafeReadback(cause)), false},
		{errors.Join(UnknownWithSafeReadback(cause), Unknown(cause)), false},
		{errors.Join(UnknownWithSafeReadback(cause), cause), false},
		{errors.Join(UnknownWithSafeReadback(cause), UnknownWithSafeReadback(cause)), true},
	} {
		if got := IsReadbackSafe(test.err); got != test.want {
			t.Fatalf("safe readback = %v, want %v: %v", got, test.want, test.err)
		}
	}
}

type emptyJoinedFinality struct{}

func (emptyJoinedFinality) Error() string   { return "no transaction evidence" }
func (emptyJoinedFinality) Unwrap() []error { return nil }

func TestEmptyJoinedFinalityDoesNotAuthorizeCompensation(t *testing.T) {
	if IsNotCommitted(emptyJoinedFinality{}) || IsReadbackSafe(emptyJoinedFinality{}) {
		t.Fatal("empty aggregate was treated as proof")
	}
}
