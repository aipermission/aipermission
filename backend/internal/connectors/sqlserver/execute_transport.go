package sqlserverconnector

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type executionCapabilities struct {
	connectors.RuntimeCapabilityResolver
	cancel context.CancelCauseFunc
}

func (c executionCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	if c.RuntimeCapabilityResolver == nil {
		return nil
	}
	capability := c.RuntimeCapabilityResolver.RuntimeCapability(name)
	if transport, ok := capability.(connectors.NetworkTransport); ok && name == connectors.NetworkTransportCapabilityName {
		return executionTransport{NetworkTransport: transport, cancel: c.cancel}
	}
	return capability
}

type executionTransport struct {
	connectors.NetworkTransport
	cancel context.CancelCauseFunc
}

func (t executionTransport) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	conn, err := t.NetworkTransport.DialConnectorTCP(ctx, request)
	if err != nil {
		return nil, err
	}
	return &executionNetworkConn{Conn: conn, cancel: t.cancel}, nil
}

// Some native read failures reach only the driver's token channel, not its
// ReturnMessage queue. Cancel the action lifetime so both the message loop and
// the existing socket context binder observe the failure, including before metadata.
type executionNetworkConn struct {
	net.Conn
	cancel context.CancelCauseFunc
}

func (c *executionNetworkConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	return n, c.observe("read", err)
}

func (c *executionNetworkConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	return n, c.observe("write", err)
}

// The driver's timeoutConn can return a deadline-setting error before calling
// Read or Write, so those failures must also wake the message consumer.
func (c *executionNetworkConn) SetDeadline(deadline time.Time) error {
	return c.observe("set deadline", c.Conn.SetDeadline(deadline))
}

func (c *executionNetworkConn) SetReadDeadline(deadline time.Time) error {
	return c.observe("set read deadline", c.Conn.SetReadDeadline(deadline))
}

func (c *executionNetworkConn) SetWriteDeadline(deadline time.Time) error {
	return c.observe("set write deadline", c.Conn.SetWriteDeadline(deadline))
}

func (c *executionNetworkConn) observe(operation string, err error) error {
	if err != nil {
		c.cancel(fmt.Errorf("SQL Server execution transport %s: %w", operation, err))
	}
	return err
}
