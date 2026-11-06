package connectors

import "time"

// Transport and clients leave room for admission, cleanup and durable results.
// Individual actions retain their own shorter execution and caller deadlines.
const (
	MaximumActionExecutionSeconds = 300
	ActionRequestTimeout          = MaximumActionExecutionSeconds*time.Second + 30*time.Second
	DefaultActionClientTimeout    = ActionRequestTimeout + 5*time.Second
)
