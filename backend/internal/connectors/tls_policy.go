package connectors

import (
	"crypto/tls"
	"net"
	"strings"
)

// UseVerifiedTLSByDefault resolves the shared "auto" transport policy. Direct
// remote endpoints use verified TLS; explicitly local endpoints and services
// reached through an SSH transport keep their customary plaintext default.
func UseVerifiedTLSByDefault(connectionMode, host string) bool {
	if strings.EqualFold(strings.TrimSpace(connectionMode), "over_ssh") {
		return false
	}

	normalized := normalizeTLSHost(host)
	if normalized == "" {
		return true
	}
	if normalized == "localhost" || strings.HasSuffix(normalized, ".localhost") {
		return false
	}
	switch normalized {
	case "host.docker.internal", "gateway.docker.internal", "host.containers.internal":
		return false
	}
	if ip := net.ParseIP(normalized); ip != nil && ip.IsLoopback() {
		return false
	}
	return true
}

// VerifiedTLSConfig returns the strict TLS baseline used by connector
// protocols that perform their own handshake over a NetworkTransport socket.
func VerifiedTLSConfig(host string) *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: normalizeTLSHost(host),
	}
}

func normalizeTLSHost(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.TrimSuffix(host, ".")
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if zoneIndex := strings.LastIndex(host, "%"); zoneIndex > -1 {
		host = host[:zoneIndex]
	}
	return host
}
