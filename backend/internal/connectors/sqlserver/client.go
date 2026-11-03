package sqlserverconnector

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
)

const queryTimeout = 20 * time.Second

func connection(ctx context.Context, runtime connectors.RuntimeContext) (*sql.DB, error) {
	if err := validateRuntime(runtime); err != nil {
		return nil, err
	}
	transport, ok := runtime.Capability(connectors.NetworkTransportCapabilityName).(connectors.NetworkTransport)
	if !ok {
		return nil, errTransport
	}
	if runtime.Secrets == nil {
		return nil, errCredential
	}
	password, err := runtime.Secrets.GetSecret(ctx, "password")
	if err != nil {
		return nil, fmt.Errorf("%w: resolve SQL Server password: %w", connectors.ErrSecretProvider, err)
	}
	if password == "" || len(password) > 4096 {
		return nil, errCredential
	}
	config, _ := connectors.NormalizeSchemaValues(New().TargetSchema(), runtime.Target.Config)
	tlsConfig, err := targetTLS(config)
	if err != nil {
		return nil, err
	}
	encryption := msdsn.Encryption(msdsn.EncryptionRequired)
	if tlsConfig == nil {
		encryption = msdsn.EncryptionDisabled
	}
	port, ok := connectors.NativeIntValue(config["port"])
	if !ok || port < 1 || port > 65535 {
		return nil, fmt.Errorf("%w: port must be between 1 and 65535", errConfig)
	}
	params := msdsn.Config{
		Host: text(config, "host"), Port: uint64(port), Database: text(config, "database"),
		User: text(runtime.Profile.Public, "username"), Password: password,
		Encryption: encryption, TLSConfig: tlsConfig, HostInCertificateProvided: true,
		AppName: "AIPermission", DisableRetry: true, Protocols: []string{"tcp"},
		ConnTimeout: queryTimeout, DialTimeout: 10 * time.Second, KeepAlive: 30 * time.Second, PacketSize: 4096,
	}
	driver := mssql.NewConnectorConfig(params)
	driver.Dialer = transportDialer{lifetime: ctx, transport: transport, request: connectors.NetworkDialRequest{
		SourceTargetRef: runtime.Target.Ref, SourceProjectID: runtime.Target.ProjectID,
		Mode: "direct", Host: params.Host, Port: port,
	}}
	// These limits belong to this connection, not the server or other clients.
	// A UTF-16 ASCII LOB uses two wire bytes per output byte. Keep the server
	// cap above twice the cell cap so server-side truncation is still reported.
	driver.SessionInitSQL = "SET QUOTED_IDENTIFIER ON; SET LOCK_TIMEOUT 5000; SET TEXTSIZE 131072;"
	db := sql.OpenDB(driver)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(0)
	db.SetConnMaxLifetime(time.Minute)
	return db, nil
}

func validateRuntime(runtime connectors.RuntimeContext) error {
	if runtime.Target.ConnectorKind != Kind || runtime.Profile.ConnectorKind != Kind || runtime.Target.ID <= 0 || runtime.Profile.ID <= 0 || runtime.Profile.TargetID != runtime.Target.ID {
		return fmt.Errorf("%w: target and profile identity must match", errConfig)
	}
	if runtime.Target.Ref != fmt.Sprintf("%s:%d:%d", Kind, runtime.Target.ID, runtime.Profile.ID) {
		return fmt.Errorf("%w: target reference does not match profile", errConfig)
	}
	if err := New().ValidateTargetConfig(runtime.Target.Config); err != nil {
		return err
	}
	return New().ValidateCredentialProfile(runtime.Profile.Kind, runtime.Profile.Public, nil, &runtime.Profile)
}

type transportDialer struct {
	lifetime  context.Context
	transport connectors.NetworkTransport
	request   connectors.NetworkDialRequest
}

func (d transportDialer) HostName() string { return d.request.Host }

func (d transportDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	sameHost := strings.EqualFold(host, d.request.Host)
	if expected := net.ParseIP(d.request.Host); expected != nil {
		sameHost = expected.Equal(net.ParseIP(host))
	}
	if err != nil || network != "tcp" || !sameHost || port != strconv.Itoa(d.request.Port) {
		return nil, fmt.Errorf("SQL Server redirect outside the configured endpoint is not allowed")
	}
	conn, err := d.transport.DialConnectorTCP(ctx, d.request)
	if err != nil {
		return nil, err
	}
	lifetime := d.lifetime
	if lifetime == nil {
		lifetime = ctx
	}
	return connectors.BindNetworkConnectionContext(lifetime, conn), nil
}
