package sqlserverconnector

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	mssql "github.com/microsoft/go-mssqldb"
)

func (Connector) TestConnection(ctx context.Context, runtime connectors.RuntimeContext) (connectors.TestResult, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	db, err := connection(ctx, runtime)
	if err == nil {
		defer db.Close()
		var version string
		err = db.QueryRowContext(ctx, "SELECT CONVERT(varchar(128), SERVERPROPERTY('ProductVersion'))").Scan(&version)
		if err == nil {
			return connectors.TestResult{Status: connectors.TestOK, Message: "SQL Server connection succeeded", Details: map[string]any{"server_version": version}}, nil
		}
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return connectors.TestResult{Status: classifyTestError(err), Message: err.Error()}, nil
}

func classifyTestError(err error) connectors.TestStatus {
	var serverError mssql.Error
	var networkError net.Error
	var verificationError *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	switch {
	case errors.Is(err, connectors.ErrSecretProvider):
		return connectors.TestUnknownError
	case errors.Is(err, errConfig), errors.Is(err, errCredential), errors.Is(err, errTransport):
		return connectors.TestFailedConfig
	case errors.As(err, &serverError):
		switch serverError.Number {
		case 18456, 18452, 18470, 18487, 18488:
			return connectors.TestFailedAuth
		case 229, 230, 262, 916, 4060:
			return connectors.TestFailedPermission
		}
	case errors.As(err, &verificationError), errors.As(err, &unknownCA), errors.As(err, &hostnameError), driverTLSError(err):
		return connectors.TestFailedTLS
	case errors.As(err, &networkError), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return connectors.TestFailedNetwork
	}
	return connectors.TestUnknownError
}

func driverTLSError(err error) bool {
	// The ordinary TDS handshake flattens certificate errors, so typed causes
	// alone are insufficient. Only known driver prefixes count, not host names.
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		message := strings.ToLower(cause.Error())
		if strings.HasPrefix(message, "tls handshake failed:") || strings.HasPrefix(message, "tls handshake flush failed:") || message == "server does not support encryption" {
			return true
		}
	}
	return false
}
