package volumes

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
)

// Plugin represents an individual CSI controller plugin
type Plugin struct {
	// name is the name of the plugin, which is also the name used as the
	// Driver.Name field
	name string

	// socket is the unix socket to connect to this plugin at.
	socket string
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Client() csi.ControllerClient {
	return nil
}
