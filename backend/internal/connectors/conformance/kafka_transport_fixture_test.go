package conformance_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

// Every bootstrap and advertised-broker dial uses the same fixture-only gate.
type kafkaFixtureTransport struct {
	host           string
	port           int
	mu             sync.Mutex
	connectorDials int
	requests       map[uint16]int
	invalidFrame   bool
}

func (f *kafkaFixtureTransport) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return f
	}
	return nil
}

func (*kafkaFixtureTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (f *kafkaFixtureTransport) dial(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" || address != net.JoinHostPort(f.host, strconv.Itoa(f.port)) {
		return nil, fmt.Errorf("broker dial is outside the owned fixture")
	}
	return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, address)
}

func (f *kafkaFixtureTransport) DialConnectorTCP(ctx context.Context, request connectors.NetworkDialRequest) (net.Conn, error) {
	if request.Mode != "direct" || request.SourceTargetRef != "kafka:1:1" || request.TransportTargetRef != "" {
		return nil, fmt.Errorf("broker transport is outside the owned fixture")
	}
	conn, err := f.dial(ctx, "tcp", net.JoinHostPort(request.Host, strconv.Itoa(request.Port)))
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.connectorDials++
	f.mu.Unlock()
	return &kafkaProbeConn{Conn: conn, transport: f}, nil
}

// Observe only the fixed request length/API prefix, never message bytes.
type kafkaProbeConn struct {
	net.Conn
	transport *kafkaFixtureTransport
	mu        sync.Mutex
	prefix    [6]byte
	used      int
	remaining int
}

func (c *kafkaProbeConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, err := c.Conn.Write(p)
	c.observeWritten(p[:n])
	return n, err
}

func (c *kafkaProbeConn) observeWritten(p []byte) {
	for len(p) > 0 {
		if c.remaining > 0 {
			consumed := min(c.remaining, len(p))
			c.remaining -= consumed
			p = p[consumed:]
			continue
		}
		consumed := copy(c.prefix[c.used:], p)
		c.used += consumed
		p = p[consumed:]
		if c.used != len(c.prefix) {
			continue
		}
		length := binary.BigEndian.Uint32(c.prefix[:4])
		if length < 2 || length > 2<<20 {
			c.transport.mu.Lock()
			c.transport.invalidFrame = true
			c.transport.mu.Unlock()
			return
		}
		c.transport.mu.Lock()
		if c.transport.requests == nil {
			c.transport.requests = map[uint16]int{}
		}
		c.transport.requests[binary.BigEndian.Uint16(c.prefix[4:])]++
		c.transport.mu.Unlock()
		c.used = 0
		c.remaining = int(length) - 2
	}
}

func (f *kafkaFixtureTransport) observed() (int, int, int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connectorDials, f.requests[8], f.requests[11], f.invalidFrame
}
