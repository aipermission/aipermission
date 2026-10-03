// Package cleanup owns bounded detached PostgreSQL network cleanup.
package cleanup

import (
	"context"
	"time"
)

const Timeout = 5 * time.Second

// Run retains request values, but gives rollback/close an independent deadline
// after the caller cancels. Callers must still close a connection after rollback.
func Run(ctx context.Context, operation func(context.Context) error) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), Timeout)
	defer cancel()
	return operation(cleanup)
}
