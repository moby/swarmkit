package networkallocator

import (
	"fmt"

	"github.com/docker/libnetwork/idm"
	"github.com/docker/swarm-v2/api"
)

const (
	// Start of the dynamic port range from which node ports will
	// be allocated when the user did not specify a port.
	dynamicPortStart = 30000

	// End of the dynamic port range from which node ports will be
	// allocated when the user did not specify a port.
	dynamicPortEnd = 32767

	// The start of master port range which will hold all the
	// allocation state of ports allocated so far regerdless of
	// whether it was user defined or not.
	masterPortStart = 1

	// The end of master port range which will hold all the
	// allocation state of ports allocated so far regerdless of
	// whether it was user defined or not.
	masterPortEnd = 65535
)

type portAllocator struct {
	// portspace definition per protocol
	portSpaces map[api.Endpoint_Protocol]*portSpace
}

type portSpace struct {
	protocol         api.Endpoint_Protocol
	masterPortSpace  *idm.Idm
	dynamicPortSpace *idm.Idm
}

func newPortAllocator() (*portAllocator, error) {
	portSpaces := make(map[api.Endpoint_Protocol]*portSpace)
	for _, protocol := range []api.Endpoint_Protocol{api.Endpoint_TCP, api.Endpoint_UDP} {
		ps, err := newPortSpace(protocol)
		if err != nil {
			return nil, err
		}

		portSpaces[protocol] = ps
	}

	return &portAllocator{portSpaces: portSpaces}, nil
}

func newPortSpace(protocol api.Endpoint_Protocol) (*portSpace, error) {
	masterName := fmt.Sprintf("%s-master-ports", protocol)
	dynamicName := fmt.Sprintf("%s-dynamic-ports", protocol)

	master, err := idm.New(nil, masterName, masterPortStart, masterPortEnd)
	if err != nil {
		return nil, err
	}

	dynamic, err := idm.New(nil, dynamicName, dynamicPortStart, dynamicPortEnd)
	if err != nil {
		return nil, err
	}

	return &portSpace{
		protocol:         protocol,
		masterPortSpace:  master,
		dynamicPortSpace: dynamic,
	}, nil
}

func (pa *portAllocator) serviceAllocatePorts(s *api.Service) (err error) {
	if s.Spec.Endpoint == nil {
		return nil
	}

	defer func() {
		if err != nil {
			// Free all the ports allocated so far which
			// should be present in s.Endpoints.Ports
			pa.serviceDeallocatePorts(s)
		}
	}()

	for _, portConfig := range s.Spec.Endpoint.Ports {
		// Make a copy of port config to create runtime state
		portState := portConfig.Copy()
		if err = pa.portSpaces[portState.Protocol].allocate(portState); err != nil {
			return
		}

		if s.Endpoint == nil {
			s.Endpoint = &api.Endpoint{}
		}

		s.Endpoint.Ports = append(s.Endpoint.Ports, portState)
	}

	return nil
}

func (pa *portAllocator) serviceDeallocatePorts(s *api.Service) {
	for _, portState := range s.Endpoint.Ports {
		pa.portSpaces[portState.Protocol].free(portState)
	}

	s.Endpoint.Ports = nil
}

func (ps *portSpace) allocate(p *api.Endpoint_PortConfiguration) (err error) {
	if p.NodePort != 0 {
		// If it falls in the dynamic port range check out
		// from dynamic port space first.
		if p.NodePort >= dynamicPortStart && p.NodePort <= dynamicPortEnd {
			if err = ps.dynamicPortSpace.GetSpecificID(uint64(p.NodePort)); err != nil {
				return err
			}

			defer func() {
				if err != nil {
					ps.dynamicPortSpace.Release(uint64(p.NodePort))
				}
			}()
		}

		return ps.masterPortSpace.GetSpecificID(uint64(p.NodePort))
	}

	// Check out an arbitrary port from dynamic port space.
	nodePort, err := ps.dynamicPortSpace.GetID()
	if err != nil {
		return
	}
	defer func() {
		if err != nil {
			ps.dynamicPortSpace.Release(uint64(nodePort))
		}
	}()

	// Make sure we allocate the same port from the master space.
	if err = ps.masterPortSpace.GetSpecificID(uint64(nodePort)); err != nil {
		return
	}

	p.NodePort = uint32(nodePort)
	return nil
}

func (ps *portSpace) free(p *api.Endpoint_PortConfiguration) {
	if p.NodePort >= dynamicPortStart && p.NodePort <= dynamicPortEnd {
		ps.dynamicPortSpace.Release(uint64(p.NodePort))
	}

	ps.masterPortSpace.Release(uint64(p.NodePort))
}
