package csi

import (
	"context"
	"errors"

	"google.golang.org/grpc"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/docker/swarmkit/api"
)

// Plugin is the interface for a CSI controller plugin
type Plugin interface {
	CreateVolume(context.Context, *api.Volume) (*api.VolumeInfo, error)
	DeleteVolume(context.Context, *api.Volume) error
	PublishVolume(context.Context, *api.Volume, string) (map[string]string, error)
	AddNode(swarmID, csiID string)
	RemoveNode(swarmID string)
}

// plugin represents an individual CSI controller plugin
type plugin struct {
	// name is the name of the plugin, which is also the name used as the
	// Driver.Name field
	name string

	// socket is the unix socket to connect to this plugin at.
	socket string

	// provider is the SecretProvider, which allows retrieving secrets for CSI
	// calls.
	provider SecretProvider

	// cc is the grpc client connection
	cc *grpc.ClientConn
	// idClient is the identity service client
	idClient csi.IdentityClient
	// controllerClient is the controller service client
	controllerClient csi.ControllerClient

	// controller indicates that the plugin has controller capabilities.
	controller bool

	// swarmToCSI maps a swarm node ID to the corresponding CSI node ID
	swarmToCSI map[string]string

	// csiToSwarm maps a CSI node ID back to the swarm node ID.
	csiToSwarm map[string]string
}

// NewPlugin creates a new Plugin object.
func NewPlugin(config *api.CSIConfig_Plugin, provider SecretProvider) Plugin {
	return &plugin{
		name:       config.Name,
		socket:     config.ControllerSocket,
		provider:   provider,
		swarmToCSI: map[string]string{},
		csiToSwarm: map[string]string{},
	}
}

// connect is a private method that sets up the identity client and controller
// client from a grpc client. it exists separately so that testing code can
// substitute in fake clients without a grpc connection
func (p *plugin) connect(ctx context.Context, cc *grpc.ClientConn) error {
	p.cc = cc
	// first, probe the plugin, to ensure that it exists and is ready to go
	idc := csi.NewIdentityClient(cc)
	p.idClient = idc

	// controllerClient may not do anything if the plugin does not support
	// the controller service, but it should not be an error to create it now
	// anyway
	p.controllerClient = csi.NewControllerClient(cc)

	return nil
}

// TODO(dperny): return error
func (p *plugin) init(ctx context.Context) error {
	probe, err := p.idClient.Probe(ctx, &csi.ProbeRequest{})
	if err != nil {
		return err
	}
	if probe.Ready != nil && !probe.Ready.Value {
		// TODO(dperny): retry?
		return nil
	}

	resp, err := p.idClient.GetPluginCapabilities(ctx, &csi.GetPluginCapabilitiesRequest{})
	if err != nil {
		// TODO(dperny): handle
		return err
	}
	if resp == nil {
		return nil
	}
	for _, c := range resp.Capabilities {
		if sc := c.GetService(); sc != nil {
			switch sc.Type {
			case csi.PluginCapability_Service_CONTROLLER_SERVICE:
				p.controller = true
			}
		}
	}

	return nil
}

// CreateVolume wraps and abstracts the CSI CreateVolume logic and returns
// the volume info, or an error.
func (p *plugin) CreateVolume(ctx context.Context, v *api.Volume) (*api.VolumeInfo, error) {
	if !p.controller {
		// TODO(dperny): come up with a scheme to handle headless plugins
		// TODO(dperny): handle plugins without create volume capabilities
		return &api.VolumeInfo{VolumeID: v.Spec.Annotations.Name}, nil
	}

	createVolumeRequest := p.makeCreateVolume(v)
	resp, err := p.Client().CreateVolume(ctx, createVolumeRequest)
	if err != nil {
		return nil, err
	}

	return makeVolumeInfo(resp.Volume), nil
}

func (p *plugin) DeleteVolume(_ context.Context, v *api.Volume) error {
	return errors.New("not yet implemented")
}

// PublishVolume calls ControllerPublishVolume to publish the given Volume to
// the Node with the given swarmkit ID. It returns a map, which is the
// PublishContext for this Volume on this Node.
func (p *plugin) PublishVolume(ctx context.Context, v *api.Volume, nodeID string) (map[string]string, error) {
	// TODO(dperny): implement
	return nil, nil
}

// AddNode adds a mapping for a node's swarm ID to the ID provided by this CSI
// plugin. This allows future calls to the plugin to be done entirely in terms
// of the swarm node ID.
//
// The CSI node ID is provided by the node as part of the NodeDescription.
func (p *plugin) AddNode(swarmID, csiID string) {
	p.swarmToCSI[swarmID] = csiID
	p.csiToSwarm[csiID] = swarmID
}

// RemoveNode removes a node from this plugin's node mappings.
func (p *plugin) RemoveNode(swarmID string) {
	csiID := p.swarmToCSI[swarmID]
	delete(p.swarmToCSI, swarmID)
	delete(p.csiToSwarm, csiID)
}

func (p *plugin) Client() csi.ControllerClient {
	return p.controllerClient
}

// makeCreateVolume makes a csi.CreateVolumeRequest from the volume object and
// spec. it uses the Plugin's SecretProvider to retrieve relevant secrets.
func (p *plugin) makeCreateVolume(v *api.Volume) *csi.CreateVolumeRequest {
	secrets := p.makeSecrets(v)
	return &csi.CreateVolumeRequest{
		Name:       v.Spec.Annotations.Name,
		Parameters: v.Spec.Driver.Options,
		VolumeCapabilities: []*csi.VolumeCapability{
			makeAccessMode(v.Spec.AccessMode),
		},
		Secrets:                   secrets,
		AccessibilityRequirements: makeTopologyRequirement(v.Spec.AccessibilityRequirements),
		CapacityRange:             makeCapacityRange(v.Spec.CapacityRange),
	}
}

// makeSecrets uses the plugin's SecretProvider to make the secrets map to pass
// to CSI RPCs.
func (p *plugin) makeSecrets(v *api.Volume) map[string]string {
	secrets := map[string]string{}
	for _, vs := range v.Spec.Secrets {
		// a secret should never be nil, but check just to be sure
		if vs != nil {
			secret := p.provider.GetSecret(vs.Secret)
			if secret != nil {
				// TODO(dperny): return an error, but this should never happen,
				// as secrets should be validated at volume creation time
				secrets[vs.Key] = string(secret.Spec.Data)
			}
		}
	}
	return secrets
}
