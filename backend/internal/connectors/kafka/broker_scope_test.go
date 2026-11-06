package kafka

import (
	"fmt"
	"strings"
	"testing"
)

func TestBrokerScopeUsesExplicitCanonicalEndpointIdentity(t *testing.T) {
	_, scope, err := configuredBrokerScope(map[string]any{
		"bootstrap_brokers":  "BROKER-A:09092,[2001:0db8:0:0::1]:9093",
		"advertised_brokers": "broker-b:9092,broker-c.:9092,[fe80::1%ZoneA]:9092,10.0.0.5:443",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"broker-a:9092", "[2001:db8::1]:9093", "BROKER-B:9092", "broker-c.:9092", "[fe80::1%ZoneA]:9092", "10.0.0.5:443"} {
		if _, _, err := scope.resolve(endpoint); err != nil {
			t.Fatalf("explicit endpoint %q rejected: %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{"broker-a:9093", "broker-b.evil:9092", "broker-c:9092", "[fe80::1%zonea]:9092", "169.254.169.254:80", "broker-a.:9092"} {
		if _, _, err := scope.resolve(endpoint); err == nil {
			t.Fatalf("unconfigured endpoint %q accepted", endpoint)
		}
	}
}

func TestBrokerScopeRejectsMalformedAndOversizedConfiguration(t *testing.T) {
	for _, endpoint := range []string{"", "broker", ":9092", "broker:0", "broker:65536", "broker:+9092", "broker: 9092", "http://broker:9092", "user@broker:9092", "*.example:9092", "10.0.0.0/8:9092", "broker/path:9092", "bröker:9092", "-broker:9092", "broker..test:9092", "127.000.0.1:9092"} {
		if err := New().ValidateTargetConfig(map[string]any{"bootstrap_brokers": endpoint}); err == nil {
			t.Fatalf("malformed endpoint accepted: %q", endpoint)
		}
	}
	for _, key := range []string{"tls_enabled", "allow_insecure_plain_sasl", "allow_insecure_scram_sasl", "bootstrap_brokers", "advertised_brokers"} {
		for _, value := range []any{nil, 1, []string{}, "true"} {
			config := map[string]any{"bootstrap_brokers": "broker:9092", key: value}
			if err := New().ValidateTargetConfig(config); err == nil {
				t.Fatalf("invalid %s type accepted: %#v", key, value)
			}
		}
	}
	config := map[string]any{"bootstrap_brokers": "broker:9092", "advertised_brokers": strings.Repeat(" ", 65537)}
	if err := New().ValidateTargetConfig(config); err == nil {
		t.Fatal("oversized additional list accepted")
	}
	var endpoints []string
	for index := range 201 {
		endpoints = append(endpoints, fmt.Sprintf("broker-%d:9092", index))
	}
	config["advertised_brokers"] = strings.Join(endpoints, "\n")
	if err := New().ValidateTargetConfig(config); err == nil {
		t.Fatal("too many additional brokers accepted")
	}
}

func TestMissingAdditionalBrokerScopeDoesNotTrustDiscovery(t *testing.T) {
	_, scope, err := configuredBrokerScope(map[string]any{"bootstrap_brokers": "broker:9092"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := scope.resolve("broker:9092"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scope.resolve("advertised:9092"); err == nil {
		t.Fatal("missing additional scope trusted an advertised broker")
	}
}
