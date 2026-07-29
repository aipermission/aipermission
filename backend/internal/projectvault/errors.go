package projectvault

import "errors"

var (
	ErrNotFound = errors.New("vault item not found")
	ErrStale    = errors.New("vault item changed; refresh and try again")
)

type ValidationError string

func (e ValidationError) Error() string {
	return string(e)
}
