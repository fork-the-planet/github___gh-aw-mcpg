package cmd

import "net"

// clientAddr returns a client-friendly address from a listener address.
// When the host is a wildcard (0.0.0.0, ::, or empty), it substitutes
// "localhost" so the printed GH_HOST value is usable from a client.
func clientAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return net.JoinHostPort("localhost", port)
	}
	return addr
}
