package sshkeys

import "errors"

var ErrNotFound = errors.New("ssh key not found")

type ValidationError string

func (e ValidationError) Error() string {
	return string(e)
}
