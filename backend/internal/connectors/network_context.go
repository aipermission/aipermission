package connectors

import (
	"context"
	"net"
)

// BindNetworkConnectionContext closes a socket when its full operation ends,
// even if a protocol library resets I/O deadlines. Pass the operation lifetime,
// not a dial-only child context that the driver cancels after connecting.
func BindNetworkConnectionContext(ctx context.Context, conn net.Conn) net.Conn {
	bound := &contextNetworkConn{Conn: conn}
	bound.stop = context.AfterFunc(ctx, func() { _ = conn.Close() })
	return bound
}

type contextNetworkConn struct {
	net.Conn
	stop func() bool
}

func (conn *contextNetworkConn) Close() error {
	conn.stop()
	return conn.Conn.Close()
}
