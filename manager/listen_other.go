// +build !linux

package manager

import "net"

// getDefaultListenIP attempts to get the default listen address by reaching out
// to a public server and getting the local address
func getDefaultListenIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		// Maybe it's ipv6 only?
		var err2 error
		conn, err2 = net.Dial("udp", "[2001:4860:4860:0:0:0:0:8888]:53")
		// keep the original error on failure
		if err2 != nil {
			return "", err
		}
	}
	conn.Close()
	addr, _, err := net.SplitHostPort(conn.LocalAddr().String())
	return addr, err
}
