package api

import (
	"errors"
	"strings"
	"testing"
)

func TestSSHConnectionFailureMessageClassifiesCommonFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "auth failure",
			err:  errors.New("ssh dial: ssh: handshake failed: ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain"),
			want: "authentication failed",
		},
		{
			name: "connection refused",
			err:  errors.New("ssh dial: dial tcp 192.0.2.10:22: connect: connection refused"),
			want: "SSH port refused",
		},
		{
			name: "timeout",
			err:  errors.New("ssh dial: dial tcp 192.0.2.10:22: i/o timeout"),
			want: "timed out",
		},
		{
			name: "unreachable",
			err:  errors.New("ssh dial: dial tcp 192.0.2.10:22: no route to host"),
			want: "not reachable",
		},
		{
			name: "host key",
			err:  errors.New("ssh dial: host key verification failed"),
			want: "host key verification failed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sshConnectionFailureMessage(test.err)
			if !strings.Contains(got, test.want) {
				t.Fatalf("message %q does not contain %q", got, test.want)
			}
		})
	}
}
