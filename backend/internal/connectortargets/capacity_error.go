package connectortargets

import (
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
)

// ActionRequestCapacityError contains only the authenticated token's numeric usage.
type ActionRequestCapacityError struct {
	Usage  actioncapacity.Usage
	Limits actioncapacity.Limits
}

func (err *ActionRequestCapacityError) Error() string {
	guidance := "Completed request records count toward storage limits; waiting alone may not restore capacity. Review retention and token usage in the local gateway."
	if err.Usage.LimitReason(err.Limits) == "running_requests" {
		guidance = "Inspect existing running requests before submitting more work; do not blindly restart or repeat dispatched commands."
	}
	return fmt.Sprintf("connector action capacity exhausted: limit=%s; projected token usage rows=%d/%d, bytes=%d/%d, running=%d/%d. %s",
		err.Usage.LimitReason(err.Limits), err.Usage.Rows, err.Limits.Rows,
		err.Usage.Bytes, err.Limits.Bytes, err.Usage.Running, err.Limits.Running, guidance)
}

func (*ActionRequestCapacityError) Unwrap() error { return ErrActionRequestCapacity }
