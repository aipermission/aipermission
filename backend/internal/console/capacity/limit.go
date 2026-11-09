package capacity

import (
	"fmt"
)

const LimitMessage = "active console session limit reached"

type LimitError struct {
	Used  int
	Cause error
}

func (e LimitError) Error() string {
	return fmt.Sprintf("%s (%d/%d). Close unused console sessions you own, or ask the operator to review Capacity in the sidebar. Waiting alone does not free a slot.", LimitMessage, e.Used, Limit)
}

func (e LimitError) Unwrap() error { return e.Cause }
