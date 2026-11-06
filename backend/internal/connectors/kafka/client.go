package kafka

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

const clientID = "aipermission-kafka-connector"

func newClient(ctx context.Context, runtime connectors.RuntimeContext, extraOptions ...kgo.Opt) (*kgo.Client, error) {
	config, err := parseClientConfig(ctx, runtime)
	if err != nil {
		return nil, err
	}
	transport, ok := runtime.Capability(connectors.NetworkTransportCapabilityName).(connectors.NetworkTransport)
	if !ok || transport == nil {
		return nil, fmt.Errorf("network transport capability is unavailable")
	}

	opts := []kgo.Opt{
		kgo.ClientID(clientID),
		kgo.SeedBrokers(config.Brokers...),
		kgo.RequestTimeoutOverhead(5 * time.Second),
		kgo.DialTimeout(10 * time.Second),
		kgo.FetchMaxBytes(1048576),
		kgo.FetchMaxPartitionBytes(1048576),
		kgo.MaxConcurrentFetches(1),
		kgo.BrokerMaxReadBytes(2097152),
		kgo.WithDecompressor(boundedDecompressor{maxBytes: maxDecompressedBatchBytes}),
		kgo.Dialer(func(dialCtx context.Context, _, address string) (net.Conn, error) {
			host, port, err := config.Scope.resolve(address)
			if err != nil {
				return nil, err
			}
			connection, err := transport.DialConnectorTCP(dialCtx, connectors.NetworkDialRequest{
				SourceTargetRef:    runtime.Target.Ref,
				SourceProjectID:    runtime.Target.ProjectID,
				Mode:               config.ConnectionMode,
				Host:               host,
				Port:               port,
				TransportTargetRef: config.TransportTargetRef,
			})
			if err != nil {
				return nil, err
			}
			if !config.TLSEnabled {
				return connection, nil
			}
			serverName := config.TLSServerName
			if serverName == "" {
				serverName = host
			}
			tlsConnection := tls.Client(connection, &tls.Config{
				MinVersion: tls.VersionTLS12,
				ServerName: serverName,
				RootCAs:    config.RootCAs,
			})
			if err := tlsConnection.HandshakeContext(dialCtx); err != nil {
				_ = connection.Close()
				return nil, fmt.Errorf("Kafka TLS handshake failed: %w", err)
			}
			return tlsConnection, nil
		}),
	}
	if config.Mechanism != nil {
		opts = append(opts, kgo.SASL(config.Mechanism))
	}
	opts = append(opts, extraOptions...)
	return kgo.NewClient(opts...)
}

type clientConfig struct {
	Brokers            []string
	Scope              brokerScope
	ConnectionMode     string
	TransportTargetRef string
	TLSEnabled         bool
	TLSServerName      string
	RootCAs            *x509.CertPool
	Mechanism          sasl.Mechanism
}

func parseClientConfig(ctx context.Context, runtime connectors.RuntimeContext) (clientConfig, error) {
	brokers, scope, err := configuredBrokerScope(runtime.Target.Config)
	if err != nil {
		return clientConfig{}, err
	}
	mode := stringValue(runtime.Target.Config, "connection_mode", "direct")
	if mode != "direct" && mode != "over_ssh" {
		return clientConfig{}, fmt.Errorf("unsupported connection mode %q", mode)
	}
	transportRef := strings.TrimSpace(stringValue(runtime.Target.Config, "transport_target_ref", ""))
	if mode == "over_ssh" && transportRef == "" {
		return clientConfig{}, fmt.Errorf("transport_target_ref is required for over_ssh mode")
	}
	tlsEnabled := boolValue(runtime.Target.Config, "tls_enabled", false)
	var roots *x509.CertPool
	if pem := strings.TrimSpace(stringValue(runtime.Target.Config, "tls_ca_pem", "")); pem != "" {
		if !tlsEnabled {
			return clientConfig{}, fmt.Errorf("tls_enabled is required when tls_ca_pem is configured")
		}
		roots, err = x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM([]byte(pem)) {
			return clientConfig{}, fmt.Errorf("tls_ca_pem does not contain a valid PEM certificate")
		}
	}
	mechanismName := stringValue(runtime.Profile.Public, "mechanism", "none")
	if err := validateSASLTransport(runtime.Target.Config, tlsEnabled, mechanismName); err != nil {
		return clientConfig{}, err
	}
	var mechanism sasl.Mechanism
	if mechanismName != "none" {
		username := strings.TrimSpace(stringValue(runtime.Profile.Public, "username", ""))
		if username == "" {
			return clientConfig{}, fmt.Errorf("username is required for SASL mechanism %s", mechanismName)
		}
		if runtime.Secrets == nil {
			return clientConfig{}, fmt.Errorf("credential secret access is unavailable")
		}
		password, secretErr := runtime.Secrets.GetSecret(ctx, "password")
		if secretErr != nil {
			return clientConfig{}, fmt.Errorf("read Kafka credential password: %w", secretErr)
		}
		if password == "" {
			return clientConfig{}, fmt.Errorf("password is required for SASL mechanism %s", mechanismName)
		}
		switch mechanismName {
		case "plain":
			mechanism = plain.Auth{User: username, Pass: password}.AsMechanism()
		case "scram_sha_256":
			mechanism = scram.Auth{User: username, Pass: password}.AsSha256Mechanism()
		case "scram_sha_512":
			mechanism = scram.Auth{User: username, Pass: password}.AsSha512Mechanism()
		default:
			return clientConfig{}, fmt.Errorf("unsupported SASL mechanism %q", mechanismName)
		}
	}
	return clientConfig{
		Brokers:            brokers,
		Scope:              scope,
		ConnectionMode:     mode,
		TransportTargetRef: transportRef,
		TLSEnabled:         tlsEnabled,
		TLSServerName:      strings.TrimSpace(stringValue(runtime.Target.Config, "tls_server_name", "")),
		RootCAs:            roots,
		Mechanism:          mechanism,
	}, nil
}

func parseBrokerList(value string) ([]string, error) {
	brokers, err := parseScopedBrokerList(value, "bootstrap", 20)
	if err != nil {
		return nil, err
	}
	if len(brokers) == 0 {
		return nil, fmt.Errorf("at least one bootstrap broker is required")
	}
	return brokers, nil
}

func splitBrokerAddress(address string) (string, int, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", 0, fmt.Errorf("broker address is empty")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, fmt.Errorf("broker %q must use host:port format: %w", address, err)
	}
	host, err = canonicalBrokerHost(host)
	if err != nil {
		return "", 0, err
	}
	for _, digit := range portText {
		if digit < '0' || digit > '9' {
			return "", 0, fmt.Errorf("broker port must be decimal digits")
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("broker %q has an invalid port", address)
	}
	return host, port, nil
}

func validateSASLTransport(config map[string]any, tlsEnabled bool, mechanism string) error {
	var exception string
	switch mechanism {
	case "none":
		return nil
	case "plain":
		exception = "allow_insecure_plain_sasl"
	case "scram_sha_256", "scram_sha_512":
		exception = "allow_insecure_scram_sasl"
	default:
		return fmt.Errorf("unsupported SASL mechanism %q", mechanism)
	}
	if !tlsEnabled && !boolValue(config, exception, false) {
		return fmt.Errorf("%s SASL requires TLS unless %s is explicitly enabled on this connector", mechanism, exception)
	}
	return nil
}

func clientBrokerMetadata(ctx context.Context, client *kgo.Client) (kadm.Metadata, error) {
	requestCtx, admin, cancel := newAdminRequest(ctx, client, 15*time.Second)
	defer cancel()
	return admin.BrokerMetadata(requestCtx)
}

func newAdminRequest(ctx context.Context, client *kgo.Client, timeout time.Duration) (context.Context, *kadm.Client, context.CancelFunc) {
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	return requestCtx, kadm.NewClient(client), cancel
}

func testFailure(err error) connectors.TestResult {
	message := err.Error()
	status := connectors.TestUnknownError
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "tls"), strings.Contains(lower, "certificate"), strings.Contains(lower, "x509"):
		status = connectors.TestFailedTLS
	case strings.Contains(lower, "sasl"), strings.Contains(lower, "authentication"), strings.Contains(lower, "credential"):
		status = connectors.TestFailedAuth
	case strings.Contains(lower, "authorization"), strings.Contains(lower, "not authorized"):
		status = connectors.TestFailedPermission
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(lower, "dial"), strings.Contains(lower, "connection refused"), strings.Contains(lower, "no such host"):
		status = connectors.TestFailedNetwork
	}
	return connectors.TestResult{Status: status, Message: message}
}

// Consumer group names are opaque broker identities, not trimmed form text.
func consumerGroup(values map[string]any) string {
	group, _ := values["group"].(string)
	return group
}

func stringValue(values map[string]any, key, fallback string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return fallback
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return fallback
	}
	return text
}

func boolValue(values map[string]any, key string, fallback bool) bool {
	value, ok := values[key]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(typed)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func intValue(values map[string]any, key string, fallback int) int {
	value, ok := values[key]
	if !ok || value == nil {
		return fallback
	}
	if parsed, ok := connectors.NativeIntValue(value); ok {
		return parsed
	}
	return fallback
}

func int64Value(values map[string]any, key string, fallback int64) int64 {
	value, ok := values[key]
	if !ok || value == nil {
		return fallback
	}
	if parsed, ok := connectors.ExactInt64Value(value); ok {
		return parsed
	}
	return fallback
}
