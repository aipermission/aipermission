package connectors

import (
	"crypto/tls"
	"testing"
)

func TestUseVerifiedTLSByDefault(t *testing.T) {
	tests := []struct {
		name string
		mode string
		host string
		want bool
	}{
		{name: "remote hostname", mode: "direct", host: "cache.example.com", want: true},
		{name: "remote IPv4", mode: "direct", host: "10.20.30.40", want: true},
		{name: "empty host fails closed", mode: "direct", host: "", want: true},
		{name: "localhost", mode: "direct", host: "localhost", want: false},
		{name: "localhost subdomain", mode: "direct", host: "redis.localhost.", want: false},
		{name: "IPv4 loopback", mode: "direct", host: "127.0.0.1", want: false},
		{name: "IPv6 loopback", mode: "direct", host: "[::1]", want: false},
		{name: "IPv6 loopback zone", mode: "direct", host: "[::1%lo]", want: false},
		{name: "Docker host alias", mode: "direct", host: "host.docker.internal", want: false},
		{name: "Podman host alias", mode: "direct", host: "host.containers.internal", want: false},
		{name: "SSH transport", mode: "over_ssh", host: "db.internal", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := UseVerifiedTLSByDefault(test.mode, test.host); got != test.want {
				t.Fatalf("UseVerifiedTLSByDefault(%q, %q) = %v, want %v", test.mode, test.host, got, test.want)
			}
		})
	}
}

func TestVerifiedTLSConfigNormalizesServerName(t *testing.T) {
	config := VerifiedTLSConfig(" [2001:db8::1%eth0] ")
	if config.MinVersion != tls.VersionTLS12 || config.ServerName != "2001:db8::1" {
		t.Fatalf("config = %#v", config)
	}
}
