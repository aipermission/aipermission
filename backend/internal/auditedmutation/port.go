// Package auditedmutation defines the transport-neutral mutation boundary used
// by application handlers that require domain state and audit evidence to
// commit atomically.
package auditedmutation

import (
	"context"
	"database/sql"
)

type Runner func(
	ctx context.Context,
	action string,
	payload func() any,
	mutate func(*sql.Tx) error,
) error
