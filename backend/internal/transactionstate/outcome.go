// Package transactionstate carries local transaction finality without making
// callers infer commit or rollback from driver error text.
package transactionstate

type failure struct {
	cause        error
	notCommitted bool
	readbackSafe bool
}

func (err *failure) Error() string { return err.cause.Error() }
func (err *failure) Unwrap() error { return err.cause }

// NotCommitted requires proof that no transaction began or that Rollback
// succeeded. ErrTxDone alone is not such proof: the transaction may have committed.
func NotCommitted(cause error) error {
	if cause == nil {
		return nil
	}
	return &failure{cause: cause, notCommitted: true}
}

// Unknown fences an ambiguous commit or rollback, including any older finality
// marker nested inside the original callback or driver error.
func Unknown(cause error) error {
	if cause == nil {
		return nil
	}
	return &failure{cause: cause}
}

// UnknownWithSafeReadback requires the transaction owner to have acknowledged
// finality or discarded its uncertain physical connection. Without this proof,
// a pooled connection could expose uncommitted rows during reconciliation.
func UnknownWithSafeReadback(cause error) error {
	if cause == nil {
		return nil
	}
	return &failure{cause: cause, readbackSafe: true}
}

func IsReadbackSafe(err error) bool {
	return hasFinality(err, func(result *failure) bool { return result.readbackSafe })
}

func IsNotCommitted(err error) bool {
	return hasFinality(err, func(result *failure) bool { return result.notCommitted })
}

func hasFinality(err error, proven func(*failure) bool) bool {
	if result, ok := err.(*failure); ok {
		return result != nil && proven(result)
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !hasFinality(child, proven) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return hasFinality(wrapped.Unwrap(), proven)
	}
	return false
}
