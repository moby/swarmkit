package manager

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// getDefaultListenIP on Linux uses netlink to get the default listen address by
// asking the kernel what the route is to a public server.
// This method is similar to what `ip route get <pubic server>` does
func getDefaultListenIP() (string, error) {
	routes, err := netlink.RouteGet(net.ParseIP("8.8.8.8"))
	if err != nil {
		var err2 error
		// maybe it's v6 only
		routes, err2 = netlink.RouteGet(net.ParseIP("2001:4860:4860:0:0:0:0:8888"))
		if err2 != nil {
			// return the original err instead of err2
			return "", err
		}
	}
	for _, r := range routes {
		// grab the first one
		if r.Src.IsGlobalUnicast() {
			return r.Src.String(), nil
		}
	}
	// could not find route
	return "", fmt.Errorf("could not determine address based on the default route")
}
