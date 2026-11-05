// Package sqlserverconnector implements SQL Server through the shared connector pipeline.
package sqlserverconnector

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"unicode/utf8"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const (
	Kind        = "sqlserver"
	Label       = "SQL Server"
	Version     = "0.2"
	defaultPort = 1433
)

var (
	errConfig     = errors.New("invalid SQL Server configuration")
	errCredential = errors.New("invalid SQL Server credential")
	errTransport  = errors.New("SQL Server network transport is unavailable")
)

type Connector struct{}

func New() Connector {
	initializeSQLGrammar()
	return Connector{}
}
func (Connector) Kind() string    { return Kind }
func (Connector) Label() string   { return Label }
func (Connector) Version() string { return Version }

func (Connector) TargetSchema() connectors.Schema {
	return connectors.Schema{Fields: []connectors.Field{
		{Name: "connection_mode", Label: "Connection mode", Type: connectors.FieldSelect, Required: true, Default: "direct", Options: []connectors.FieldOption{{Value: "direct", Label: "Direct"}}},
		{Name: "host", Label: "Host", Type: connectors.FieldString, Required: true, Description: "SQL Server TCP hostname or IP address; not a connection string or instance name."},
		{Name: "port", Label: "TCP port", Type: connectors.FieldInteger, Required: true, Default: defaultPort, Description: "Explicit SQL Server TCP port. Named-instance discovery is not used."},
		{Name: "database", Label: "Database", Type: connectors.FieldString, Required: true, PreserveWhitespace: true},
		{Name: "tls_mode", Label: "TLS mode", Type: connectors.FieldSelect, Required: true, Default: "verify_full", Options: []connectors.FieldOption{{Value: "verify_full", Label: "Verify full"}, {Value: "disable", Label: "Disable (unencrypted)"}}},
		{Name: "tls_server_name", Label: "TLS server name", Type: connectors.FieldString, Description: "Optional certificate hostname when connecting to an IP address."},
		{Name: "ca_certificate", Label: "Trusted CA certificate", Type: connectors.FieldMultiline, PreserveWhitespace: true, Description: "Optional public PEM CA certificate for private certificate authorities. No private keys."},
	}}
}

func (Connector) CredentialSchemas() []connectors.CredentialSchema {
	return []connectors.CredentialSchema{{Kind: "username_password", Label: "SQL authentication", Description: "Dedicated SQL Server login, not Windows integrated authentication.", Schema: connectors.Schema{Fields: []connectors.Field{
		{Name: "username", Label: "Username", Type: connectors.FieldString, Required: true},
		{Name: "password", Label: "Password", Type: connectors.FieldSecret, Secret: true, Required: true},
	}}}}
}

func (Connector) ValidateTargetConfig(config map[string]any) error {
	normalized, err := connectors.NormalizeSchemaValues(New().TargetSchema(), config)
	if err != nil {
		return fmt.Errorf("%w: %v", errConfig, err)
	}
	host := text(normalized, "host")
	if len(host) > 253 || strings.ContainsAny(host, "\\/:;?@#[] \t\r\n\x00") && net.ParseIP(host) == nil {
		return fmt.Errorf("%w: use a hostname or IP and a separate TCP port", errConfig)
	}
	if _, err := identifier(text(normalized, "database")); err != nil {
		return err
	}
	port, ok := normalized["port"].(int64)
	if !ok || port < 1 || port > 65535 {
		return fmt.Errorf("%w: port must be between 1 and 65535", errConfig)
	}
	_, err = targetTLS(normalized)
	return err
}

func (Connector) ValidateCredentialProfileInput(kind string, public, submittedSecret map[string]any, previous *connectors.CredentialProfileView) error {
	return New().ValidateCredentialProfile(kind, public, submittedSecret, previous)
}

func (Connector) ValidateCredentialProfile(kind string, public, secret map[string]any, previous *connectors.CredentialProfileView) error {
	username := text(public, "username")
	if kind != "username_password" || username == "" || len(username) > 128 || strings.ContainsAny(username, "\\\x00\r\n") {
		return fmt.Errorf("%w: a SQL authentication username is required", errCredential)
	}
	password, present := secret["password"]
	passwordText, passwordIsString := password.(string)
	if present && (!passwordIsString || passwordText == "" || len(passwordText) > 4096) {
		return fmt.Errorf("%w: password is required and must not exceed 4096 bytes", errCredential)
	}
	if !present && (previous == nil || text(previous.Public, "username") != username) {
		return fmt.Errorf("%w: supply a password when creating a profile or changing its username", errCredential)
	}
	return nil
}

func targetTLS(config map[string]any) (*tls.Config, error) {
	mode := text(config, "tls_mode")
	if mode == "" {
		mode = "verify_full"
	}
	if mode == "disable" {
		if text(config, "ca_certificate") != "" || text(config, "tls_server_name") != "" {
			return nil, fmt.Errorf("%w: certificate fields require verified TLS", errConfig)
		}
		return nil, nil
	}
	if mode != "verify_full" {
		return nil, fmt.Errorf("%w: unsupported TLS mode", errConfig)
	}
	name := text(config, "tls_server_name")
	if name == "" {
		name = text(config, "host")
	}
	if len(name) > 253 || strings.ContainsAny(name, "\\/;?@#[] \t\r\n\x00") {
		return nil, fmt.Errorf("%w: invalid TLS server name", errConfig)
	}
	result := connectors.VerifiedTLSConfig(name)
	certificate := text(config, "ca_certificate")
	if certificate != "" {
		if len(certificate) > 64<<10 || strings.Contains(certificate, "PRIVATE KEY") {
			return nil, fmt.Errorf("%w: supply only public PEM CA certificates up to 64 KiB", errConfig)
		}
		result.RootCAs = x509.NewCertPool()
		if !result.RootCAs.AppendCertsFromPEM([]byte(certificate)) {
			return nil, fmt.Errorf("%w: invalid PEM CA certificate", errConfig)
		}
	}
	return result, nil
}

func text(values map[string]any, name string) string {
	value, _ := values[name].(string)
	return value
}

func identifier(value string) (string, error) {
	if value == "" || utf8.RuneCountInString(value) > 128 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return "", fmt.Errorf("%w: SQL identifiers must contain 1 to 128 characters without null bytes", errConfig)
	}
	return value, nil
}
