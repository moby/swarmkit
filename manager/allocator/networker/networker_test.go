package networker

import (
	"net"
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
)

func newNetworker(t *testing.T) *Networker {
	nwkr, err := New()
	assert.NoError(t, err)
	assert.NotNil(t, nwkr)
	return nwkr
}

func TestNew(t *testing.T) {
	newNetworker(t)
}

func TestNetworkAllocateInvalidIPAM(t *testing.T) {
	nwkr := newNetworker(t)
	n := &api.Network{
		ID: "testID",
		Spec: &api.NetworkSpec{
			Meta: api.Meta{
				Name: "test",
			},
			DriverConfiguration: &api.Driver{},
			IPAM: &api.NetworkSpec_IPAMOptions{
				Driver: &api.Driver{
					Name: "invalidipam,",
				},
			},
		},
	}
	err := nwkr.NetworkAllocate(n)
	assert.Error(t, err)
}

func TestNetworkAllocateInvalidDriver(t *testing.T) {
	nwkr := newNetworker(t)
	n := &api.Network{
		ID: "testID",
		Spec: &api.NetworkSpec{
			Meta: api.Meta{
				Name: "test",
			},
			DriverConfiguration: &api.Driver{
				Name: "invaliddriver",
			},
		},
	}

	err := nwkr.NetworkAllocate(n)
	assert.Error(t, err)
}

func TestNetworkDoubleAllocate(t *testing.T) {
	nwkr := newNetworker(t)
	n := &api.Network{
		ID: "testID",
		Spec: &api.NetworkSpec{
			Meta: api.Meta{
				Name: "test",
			},
		},
	}

	err := nwkr.NetworkAllocate(n)
	assert.NoError(t, err)

	err = nwkr.NetworkAllocate(n)
	assert.Error(t, err)
}

func TestNetworkAllocateEmptyConfig(t *testing.T) {
	nwkr := newNetworker(t)
	n := &api.Network{
		ID: "testID",
		Spec: &api.NetworkSpec{
			Meta: api.Meta{
				Name: "test",
			},
		},
	}

	err := nwkr.NetworkAllocate(n)
	assert.NoError(t, err)
	assert.NotEqual(t, n.Spec.IPAM.IPv4, nil)
	assert.Equal(t, len(n.Spec.IPAM.IPv6), 0)
	assert.Equal(t, len(n.Spec.IPAM.IPv4), 1)
	assert.Equal(t, n.Spec.IPAM.IPv4[0].Range, "")
	assert.Equal(t, len(n.Spec.IPAM.IPv4[0].Reserved), 0)

	_, _, err = net.ParseCIDR(n.Spec.IPAM.IPv4[0].Subnet)
	assert.NoError(t, err)

	ip := net.ParseIP(n.Spec.IPAM.IPv4[0].Gateway)
	assert.NotEqual(t, ip, nil)
}

func TestNetworkAllocateWithOneSubnet(t *testing.T) {
	nwkr := newNetworker(t)
	n := &api.Network{
		ID: "testID",
		Spec: &api.NetworkSpec{
			Meta: api.Meta{
				Name: "test",
			},
			DriverConfiguration: &api.Driver{},
			IPAM: &api.NetworkSpec_IPAMOptions{
				Driver: &api.Driver{},
				IPv4: []*api.IPAMConfiguration{
					{
						Subnet: "192.168.1.0/24",
					},
				},
			},
		},
	}

	err := nwkr.NetworkAllocate(n)
	assert.NoError(t, err)
	assert.Equal(t, len(n.Spec.IPAM.IPv6), 0)
	assert.Equal(t, len(n.Spec.IPAM.IPv4), 1)
	assert.Equal(t, n.Spec.IPAM.IPv4[0].Range, "")
	assert.Equal(t, len(n.Spec.IPAM.IPv4[0].Reserved), 0)
	assert.Equal(t, n.Spec.IPAM.IPv4[0].Subnet, "192.168.1.0/24")

	ip := net.ParseIP(n.Spec.IPAM.IPv4[0].Gateway)
	assert.NotEqual(t, ip, nil)
}

func TestNetworkAllocateWithOneSubnetGateway(t *testing.T) {
	nwkr := newNetworker(t)
	n := &api.Network{
		ID: "testID",
		Spec: &api.NetworkSpec{
			Meta: api.Meta{
				Name: "test",
			},
			DriverConfiguration: &api.Driver{},
			IPAM: &api.NetworkSpec_IPAMOptions{
				Driver: &api.Driver{},
				IPv4: []*api.IPAMConfiguration{
					{
						Subnet:  "192.168.1.0/24",
						Gateway: "192.168.1.1",
					},
				},
			},
		},
	}

	err := nwkr.NetworkAllocate(n)
	assert.NoError(t, err)
	assert.Equal(t, len(n.Spec.IPAM.IPv6), 0)
	assert.Equal(t, len(n.Spec.IPAM.IPv4), 1)
	assert.Equal(t, n.Spec.IPAM.IPv4[0].Range, "")
	assert.Equal(t, len(n.Spec.IPAM.IPv4[0].Reserved), 0)
	assert.Equal(t, n.Spec.IPAM.IPv4[0].Subnet, "192.168.1.0/24")
	assert.Equal(t, n.Spec.IPAM.IPv4[0].Gateway, "192.168.1.1")
}

func TestNetworkAllocateWithOneSubnetInvalidGateway(t *testing.T) {
	nwkr := newNetworker(t)
	n := &api.Network{
		ID: "testID",
		Spec: &api.NetworkSpec{
			Meta: api.Meta{
				Name: "test",
			},
			DriverConfiguration: &api.Driver{},
			IPAM: &api.NetworkSpec_IPAMOptions{
				Driver: &api.Driver{},
				IPv4: []*api.IPAMConfiguration{
					{
						Subnet:  "192.168.1.0/24",
						Gateway: "192.168.2.1",
					},
				},
			},
		},
	}

	err := nwkr.NetworkAllocate(n)
	assert.Error(t, err)
}

func TestNetworkAllocateWithInvalidSubnet(t *testing.T) {
	nwkr := newNetworker(t)
	n := &api.Network{
		ID: "testID",
		Spec: &api.NetworkSpec{
			Meta: api.Meta{
				Name: "test",
			},
			DriverConfiguration: &api.Driver{},
			IPAM: &api.NetworkSpec_IPAMOptions{
				Driver: &api.Driver{},
				IPv4: []*api.IPAMConfiguration{
					{
						Subnet: "1.1.1.1/32",
					},
				},
			},
		},
	}

	err := nwkr.NetworkAllocate(n)
	assert.Error(t, err)
}

func TestNetworkAllocateWithTwoSubnetsNoGateway(t *testing.T) {
	nwkr := newNetworker(t)
	n := &api.Network{
		ID: "testID",
		Spec: &api.NetworkSpec{
			Meta: api.Meta{
				Name: "test",
			},
			DriverConfiguration: &api.Driver{},
			IPAM: &api.NetworkSpec_IPAMOptions{
				Driver: &api.Driver{},
				IPv4: []*api.IPAMConfiguration{
					{
						Subnet: "192.168.1.0/24",
					},
					{
						Subnet: "192.168.2.0/24",
					},
				},
			},
		},
	}

	err := nwkr.NetworkAllocate(n)
	assert.NoError(t, err)
	assert.Equal(t, len(n.Spec.IPAM.IPv6), 0)
	assert.Equal(t, len(n.Spec.IPAM.IPv4), 2)
	assert.Equal(t, n.Spec.IPAM.IPv4[0].Range, "")
	assert.Equal(t, len(n.Spec.IPAM.IPv4[0].Reserved), 0)
	assert.Equal(t, n.Spec.IPAM.IPv4[0].Subnet, "192.168.1.0/24")
	assert.Equal(t, n.Spec.IPAM.IPv4[1].Range, "")
	assert.Equal(t, len(n.Spec.IPAM.IPv4[1].Reserved), 0)
	assert.Equal(t, n.Spec.IPAM.IPv4[1].Subnet, "192.168.2.0/24")

	ip := net.ParseIP(n.Spec.IPAM.IPv4[0].Gateway)
	assert.NotEqual(t, ip, nil)
	ip = net.ParseIP(n.Spec.IPAM.IPv4[1].Gateway)
	assert.NotEqual(t, ip, nil)
}

func TestNetworkFree(t *testing.T) {
	nwkr := newNetworker(t)
	n := &api.Network{
		ID: "testID",
		Spec: &api.NetworkSpec{
			Meta: api.Meta{
				Name: "test",
			},
			DriverConfiguration: &api.Driver{},
			IPAM: &api.NetworkSpec_IPAMOptions{
				Driver: &api.Driver{},
				IPv4: []*api.IPAMConfiguration{
					{
						Subnet:  "192.168.1.0/24",
						Gateway: "192.168.1.1",
					},
				},
			},
		},
	}

	err := nwkr.NetworkAllocate(n)
	assert.NoError(t, err)

	err = nwkr.NetworkFree(n)
	assert.NoError(t, err)

	// Reallocate again to make sure it succeeds.
	err = nwkr.NetworkAllocate(n)
	assert.NoError(t, err)
}

func TestEndpointsAllocateFree(t *testing.T) {
	nwkr := newNetworker(t)
	n1 := &api.Network{
		ID: "testID1",
		Spec: &api.NetworkSpec{
			Meta: api.Meta{
				Name: "test1",
			},
			DriverConfiguration: &api.Driver{},
			IPAM: &api.NetworkSpec_IPAMOptions{
				Driver: &api.Driver{},
				IPv4: []*api.IPAMConfiguration{
					{
						Subnet:  "192.168.1.0/24",
						Gateway: "192.168.1.1",
					},
				},
			},
		},
	}

	n2 := &api.Network{
		ID: "testID2",
		Spec: &api.NetworkSpec{
			Meta: api.Meta{
				Name: "test2",
			},
			DriverConfiguration: &api.Driver{},
			IPAM: &api.NetworkSpec_IPAMOptions{
				Driver: &api.Driver{},
				IPv4: []*api.IPAMConfiguration{
					{
						Subnet:  "192.168.2.0/24",
						Gateway: "192.168.2.1",
					},
				},
			},
		},
	}

	task := &api.Task{
		Networks: []*api.Task_NetworkAttachment{
			{
				Network: n1,
			},
			{
				Network: n2,
			},
		},
	}

	err := nwkr.NetworkAllocate(n1)
	assert.NoError(t, err)

	err = nwkr.NetworkAllocate(n2)
	assert.NoError(t, err)

	err = nwkr.EndpointsAllocate(task)
	assert.NoError(t, err)
	assert.Equal(t, len(task.Networks[0].Addresses), 1)
	assert.Equal(t, len(task.Networks[1].Addresses), 1)

	_, subnet1, _ := net.ParseCIDR("192.168.1.0/24")
	_, subnet2, _ := net.ParseCIDR("192.168.2.0/24")

	ip1, _, err := net.ParseCIDR(task.Networks[0].Addresses[0])
	assert.NoError(t, err)

	ip2, _, err := net.ParseCIDR(task.Networks[1].Addresses[0])
	assert.NoError(t, err)

	assert.Equal(t, subnet1.Contains(ip1), true)
	assert.Equal(t, subnet2.Contains(ip2), true)

	err = nwkr.EndpointsFree(task)
	assert.NoError(t, err)
	assert.Equal(t, len(task.Networks[0].Addresses), 0)
	assert.Equal(t, len(task.Networks[1].Addresses), 0)

	// Try allocation after free
	err = nwkr.EndpointsAllocate(task)
	assert.NoError(t, err)
	assert.Equal(t, len(task.Networks[0].Addresses), 1)
	assert.Equal(t, len(task.Networks[1].Addresses), 1)

	ip1, _, err = net.ParseCIDR(task.Networks[0].Addresses[0])
	assert.NoError(t, err)

	ip2, _, err = net.ParseCIDR(task.Networks[1].Addresses[0])
	assert.NoError(t, err)

	assert.Equal(t, subnet1.Contains(ip1), true)
	assert.Equal(t, subnet2.Contains(ip2), true)

	err = nwkr.EndpointsFree(task)
	assert.NoError(t, err)
	assert.Equal(t, len(task.Networks[0].Addresses), 0)
	assert.Equal(t, len(task.Networks[1].Addresses), 0)

	// Try to free endpoints on an already freed task
	err = nwkr.EndpointsFree(task)
	assert.NoError(t, err)
}
