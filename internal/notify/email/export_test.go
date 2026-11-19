package email

import (
	"context"
	"net"
)

// SetDial replaces the function n uses to open its TCP connection, so a test
// can point a host name that is not loopback at a local fake server.
func SetDial(n *Notifier, dial func(ctx context.Context, network, addr string) (net.Conn, error)) {
	n.dial = dial
}
