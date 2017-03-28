package networkallocator

import (
	"fmt"
	"net"
	"testing"

	"github.com/docker/libnetwork/discoverapi"
	"github.com/docker/libnetwork/types"
	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
)

func newNetworkAllocator(t *testing.T) *NetworkAllocator {
	na, err := New(nil)
	assert.NoError(t, err)
	assert.NotNil(t, na)
	return na
}

func TestNew(t *testing.T) {
	newNetworkAllocator(t)
}

func testAllocateInvalidIPAM(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)
	n := &api.Network{
		ID: "testID",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	dc := &api.Driver{}
	ipam := &api.IPAMOptions{
		Driver: &api.Driver{
			Name: "invalidipam,",
		},
	}
	if compat {
		n.Spec.CNMCompatDriverConfig = dc
		n.Spec.CNMCompatIPAM = ipam
	} else {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				DriverConfig: dc,
				IPAM:         ipam,
			},
		}
	}
	err := na.Allocate(n)
	assert.Error(t, err)
}
func TestAllocateInvalidIPAM(t *testing.T) {
	testAllocateInvalidIPAM(t, false)
}
func TestAllocateInvalidIPAMCompat(t *testing.T) {
	testAllocateInvalidIPAM(t, true)
}

func testAllocateInvalidDriver(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)
	n := &api.Network{
		ID: "testID",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	dc := &api.Driver{
		Name: "invaliddriver",
	}
	if compat {
		n.Spec.CNMCompatDriverConfig = dc
	} else {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				DriverConfig: dc,
			},
		}
	}
	err := na.Allocate(n)
	assert.Error(t, err)
}
func TestAllocateInvalidDriver(t *testing.T) {
	testAllocateInvalidDriver(t, false)
}
func TestAllocateInvalidDriverCompat(t *testing.T) {
	testAllocateInvalidDriver(t, true)
}

func testNetworkDoubleAllocate(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)
	n := &api.Network{
		ID: "testID",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	if !compat {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{},
		}
	}

	err := na.Allocate(n)
	assert.NoError(t, err)

	err = na.Allocate(n)
	assert.Error(t, err)
}
func TestNetworkDoubleAllocate(t *testing.T) {
	testNetworkDoubleAllocate(t, false)
}
func TestNetworkDoubleAllocateCompat(t *testing.T) {
	testNetworkDoubleAllocate(t, true)
}

func testAllocateEmptyConfig(t *testing.T, compat bool) {
	na1 := newNetworkAllocator(t)
	na2 := newNetworkAllocator(t)
	n1 := &api.Network{
		ID: "testID1",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test1",
			},
		},
	}
	if !compat {
		n1.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{},
		}
	}

	n2 := &api.Network{
		ID: "testID2",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test2",
			},
		},
	}
	if !compat {
		n2.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{},
		}
	}

	err := na1.Allocate(n1)
	assert.NoError(t, err)
	switch n1.State.(type) {
	case *api.Network_CNM:
		assert.False(t, compat, "Compat CNM network has State.CMN")
	case nil:
		assert.True(t, compat, "CNM network has no State.CNM")
	default:
		assert.Fail(t, "Network has unexpected State")

	}
	cnmState1 := n1.GetCNMCompat()
	assert.NotNil(t, cnmState1)
	assert.NotEqual(t, cnmState1.IPAM.Configs, nil)
	assert.Equal(t, len(cnmState1.IPAM.Configs), 1)
	assert.Equal(t, cnmState1.IPAM.Configs[0].Range, "")
	assert.Equal(t, len(cnmState1.IPAM.Configs[0].Reserved), 0)

	_, subnet11, err := net.ParseCIDR(cnmState1.IPAM.Configs[0].Subnet)
	assert.NoError(t, err)

	gwip11 := net.ParseIP(cnmState1.IPAM.Configs[0].Gateway)
	assert.NotEqual(t, gwip11, nil)

	err = na1.Allocate(n2)
	assert.NoError(t, err)
	switch n2.State.(type) {
	case *api.Network_CNM:
		assert.False(t, compat, "Compat CNM network has State.CMN")
	case nil:
		assert.True(t, compat, "CNM network has no State.CNM")
	default:
		assert.Fail(t, "Network has unexpected State")

	}
	cnmState2 := n2.GetCNMCompat()
	assert.NotNil(t, cnmState2)
	assert.NotEqual(t, cnmState2.IPAM.Configs, nil)
	assert.Equal(t, len(cnmState2.IPAM.Configs), 1)
	assert.Equal(t, cnmState2.IPAM.Configs[0].Range, "")
	assert.Equal(t, len(cnmState2.IPAM.Configs[0].Reserved), 0)

	_, subnet21, err := net.ParseCIDR(cnmState2.IPAM.Configs[0].Subnet)
	assert.NoError(t, err)

	gwip21 := net.ParseIP(cnmState2.IPAM.Configs[0].Gateway)
	assert.NotEqual(t, gwip21, nil)

	// Allocate n1 ans n2 with another allocator instance but in
	// intentionally reverse order.
	err = na2.Allocate(n2)
	assert.NoError(t, err)
	assert.NotEqual(t, cnmState2.IPAM.Configs, nil)
	assert.Equal(t, len(cnmState2.IPAM.Configs), 1)
	assert.Equal(t, cnmState2.IPAM.Configs[0].Range, "")
	assert.Equal(t, len(cnmState2.IPAM.Configs[0].Reserved), 0)

	_, subnet22, err := net.ParseCIDR(cnmState2.IPAM.Configs[0].Subnet)
	assert.NoError(t, err)
	assert.Equal(t, subnet21, subnet22)

	gwip22 := net.ParseIP(cnmState2.IPAM.Configs[0].Gateway)
	assert.Equal(t, gwip21, gwip22)

	err = na2.Allocate(n1)
	assert.NoError(t, err)
	assert.NotEqual(t, cnmState1.IPAM.Configs, nil)
	assert.Equal(t, len(cnmState1.IPAM.Configs), 1)
	assert.Equal(t, cnmState1.IPAM.Configs[0].Range, "")
	assert.Equal(t, len(cnmState1.IPAM.Configs[0].Reserved), 0)

	_, subnet12, err := net.ParseCIDR(cnmState1.IPAM.Configs[0].Subnet)
	assert.NoError(t, err)
	assert.Equal(t, subnet11, subnet12)

	gwip12 := net.ParseIP(cnmState1.IPAM.Configs[0].Gateway)
	assert.Equal(t, gwip11, gwip12)
}
func TestAllocateEmptyConfig(t *testing.T) {
	testAllocateEmptyConfig(t, false)
}
func TestAllocateEmptyConfigCompat(t *testing.T) {
	testAllocateEmptyConfig(t, true)
}

func testAllocateWithOneSubnet(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)
	n := &api.Network{
		ID: "testID",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	dc := &api.Driver{}
	ipam := &api.IPAMOptions{
		Driver: &api.Driver{},
		Configs: []*api.IPAMConfig{
			{
				Subnet: "192.168.1.0/24",
			},
		},
	}
	if compat {
		n.Spec.CNMCompatDriverConfig = dc
		n.Spec.CNMCompatIPAM = ipam
	} else {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				DriverConfig: dc,
				IPAM:         ipam,
			},
		}
	}

	err := na.Allocate(n)
	assert.NoError(t, err)
	switch n.State.(type) {
	case *api.Network_CNM:
		assert.False(t, compat, "Compat CNM network has State.CMN")
	case nil:
		assert.True(t, compat, "CNM network has no State.CNM")
	default:
		assert.Fail(t, "Network has unexpected State")

	}
	cnmState := n.GetCNMCompat()
	assert.NotNil(t, cnmState)
	assert.Equal(t, len(cnmState.IPAM.Configs), 1)
	assert.Equal(t, cnmState.IPAM.Configs[0].Range, "")
	assert.Equal(t, len(cnmState.IPAM.Configs[0].Reserved), 0)
	assert.Equal(t, cnmState.IPAM.Configs[0].Subnet, "192.168.1.0/24")

	ip := net.ParseIP(cnmState.IPAM.Configs[0].Gateway)
	assert.NotEqual(t, ip, nil)
}
func TestAllocateWithOneSubnet(t *testing.T) {
	testAllocateWithOneSubnet(t, false)
}
func TestAllocateWithOneSubnetCompat(t *testing.T) {
	testAllocateWithOneSubnet(t, true)
}

func testAllocateWithOneSubnetGateway(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)
	n := &api.Network{
		ID: "testID",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	dc := &api.Driver{}
	ipam := &api.IPAMOptions{
		Driver: &api.Driver{},
		Configs: []*api.IPAMConfig{
			{
				Subnet:  "192.168.1.0/24",
				Gateway: "192.168.1.1",
			},
		},
	}
	if compat {
		n.Spec.CNMCompatDriverConfig = dc
		n.Spec.CNMCompatIPAM = ipam
	} else {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				DriverConfig: dc,
				IPAM:         ipam,
			},
		}
	}

	err := na.Allocate(n)
	assert.NoError(t, err)
	switch n.State.(type) {
	case *api.Network_CNM:
		assert.False(t, compat, "Compat CNM network has State.CMN")
	case nil:
		assert.True(t, compat, "CNM network has no State.CNM")
	default:
		assert.Fail(t, "Network has unexpected State")

	}
	cnmState := n.GetCNMCompat()
	assert.NotNil(t, cnmState)
	assert.Equal(t, len(cnmState.IPAM.Configs), 1)
	assert.Equal(t, cnmState.IPAM.Configs[0].Range, "")
	assert.Equal(t, len(cnmState.IPAM.Configs[0].Reserved), 0)
	assert.Equal(t, cnmState.IPAM.Configs[0].Subnet, "192.168.1.0/24")
	assert.Equal(t, cnmState.IPAM.Configs[0].Gateway, "192.168.1.1")
}
func TestAllocateWithOneSubnetGateway(t *testing.T) {
	testAllocateWithOneSubnetGateway(t, false)
}
func TestAllocateWithOneSubnetGatewayCompat(t *testing.T) {
	testAllocateWithOneSubnetGateway(t, true)
}

func testAllocateWithOneSubnetInvalidGateway(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)
	n := &api.Network{
		ID: "testID",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	dc := &api.Driver{}
	ipam := &api.IPAMOptions{
		Driver: &api.Driver{},
		Configs: []*api.IPAMConfig{
			{
				Subnet:  "192.168.1.0/24",
				Gateway: "192.168.2.1",
			},
		},
	}
	if compat {
		n.Spec.CNMCompatDriverConfig = dc
		n.Spec.CNMCompatIPAM = ipam
	} else {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				DriverConfig: dc,
				IPAM:         ipam,
			},
		}
	}
	err := na.Allocate(n)
	assert.Error(t, err)
}
func TestAllocateWithOneSubnetInvalidGateway(t *testing.T) {
	testAllocateWithOneSubnetInvalidGateway(t, false)
}
func TestAllocateWithOneSubnetInvalidGatewayCompat(t *testing.T) {
	testAllocateWithOneSubnetInvalidGateway(t, true)
}

func testAllocateWithInvalidSubnet(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)
	n := &api.Network{
		ID: "testID",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	dc := &api.Driver{}
	ipam := &api.IPAMOptions{
		Driver: &api.Driver{},
		Configs: []*api.IPAMConfig{
			{
				Subnet: "1.1.1.1/32",
			},
		},
	}
	if compat {
		n.Spec.CNMCompatDriverConfig = dc
		n.Spec.CNMCompatIPAM = ipam
	} else {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				DriverConfig: dc,
				IPAM:         ipam,
			},
		}
	}

	err := na.Allocate(n)
	assert.Error(t, err)
}
func TestAllocateWithInvalidSubnet(t *testing.T) {
	testAllocateWithInvalidSubnet(t, false)
}
func TestAllocateWithInvalidSubnetCompat(t *testing.T) {
	testAllocateWithInvalidSubnet(t, true)
}

func testAllocateWithTwoSubnetsNoGateway(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)
	n := &api.Network{
		ID: "testID",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	dc := &api.Driver{}
	ipam := &api.IPAMOptions{
		Driver: &api.Driver{},
		Configs: []*api.IPAMConfig{
			{
				Subnet: "192.168.1.0/24",
			},
			{
				Subnet: "192.168.2.0/24",
			},
		},
	}
	if compat {
		n.Spec.CNMCompatDriverConfig = dc
		n.Spec.CNMCompatIPAM = ipam
	} else {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				DriverConfig: dc,
				IPAM:         ipam,
			},
		}
	}

	err := na.Allocate(n)
	assert.NoError(t, err)
	switch n.State.(type) {
	case *api.Network_CNM:
		assert.False(t, compat, "Compat CNM network has State.CMN")
	case nil:
		assert.True(t, compat, "CNM network has no State.CNM")
	default:
		assert.Fail(t, "Network has unexpected State")

	}
	cnmState := n.GetCNMCompat()
	assert.NotNil(t, cnmState)
	assert.Equal(t, len(cnmState.IPAM.Configs), 2)
	assert.Equal(t, cnmState.IPAM.Configs[0].Range, "")
	assert.Equal(t, len(cnmState.IPAM.Configs[0].Reserved), 0)
	assert.Equal(t, cnmState.IPAM.Configs[0].Subnet, "192.168.1.0/24")
	assert.Equal(t, cnmState.IPAM.Configs[1].Range, "")
	assert.Equal(t, len(cnmState.IPAM.Configs[1].Reserved), 0)
	assert.Equal(t, cnmState.IPAM.Configs[1].Subnet, "192.168.2.0/24")

	ip := net.ParseIP(cnmState.IPAM.Configs[0].Gateway)
	assert.NotEqual(t, ip, nil)
	ip = net.ParseIP(cnmState.IPAM.Configs[1].Gateway)
	assert.NotEqual(t, ip, nil)
}
func TestAllocateWithTwoSubnetsNoGateway(t *testing.T) {
	testAllocateWithTwoSubnetsNoGateway(t, false)
}
func TestAllocateWithTwoSubnetsNoGatewayCompat(t *testing.T) {
	testAllocateWithTwoSubnetsNoGateway(t, true)
}

func testFree(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)
	n := &api.Network{
		ID: "testID",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	dc := &api.Driver{}
	ipam := &api.IPAMOptions{
		Driver: &api.Driver{},
		Configs: []*api.IPAMConfig{
			{
				Subnet:  "192.168.1.0/24",
				Gateway: "192.168.1.1",
			},
		},
	}
	if compat {
		n.Spec.CNMCompatDriverConfig = dc
		n.Spec.CNMCompatIPAM = ipam
	} else {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				DriverConfig: dc,
				IPAM:         ipam,
			},
		}
	}

	err := na.Allocate(n)
	assert.NoError(t, err)

	err = na.Deallocate(n)
	assert.NoError(t, err)

	// Reallocate again to make sure it succeeds.
	err = na.Allocate(n)
	assert.NoError(t, err)
}
func TestFree(t *testing.T) {
	testFree(t, false)
}
func TestFreeCompat(t *testing.T) {
	testFree(t, true)
}

func testAllocateTaskFree(t *testing.T, compat bool) {
	na1 := newNetworkAllocator(t)
	na2 := newNetworkAllocator(t)
	n1 := &api.Network{
		ID: "testID1",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test1",
			},
		},
	}
	dc1 := &api.Driver{}
	ipam1 := &api.IPAMOptions{
		Driver: &api.Driver{},
		Configs: []*api.IPAMConfig{
			{
				Subnet:  "192.168.1.0/24",
				Gateway: "192.168.1.1",
			},
		},
	}
	if compat {
		n1.Spec.CNMCompatDriverConfig = dc1
		n1.Spec.CNMCompatIPAM = ipam1
	} else {
		n1.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				DriverConfig: dc1,
				IPAM:         ipam1,
			},
		}
	}

	n2 := &api.Network{
		ID: "testID2",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test2",
			},
		},
	}
	dc2 := &api.Driver{}
	ipam2 := &api.IPAMOptions{
		Driver: &api.Driver{},
		Configs: []*api.IPAMConfig{
			{
				Subnet:  "192.168.2.0/24",
				Gateway: "192.168.2.1",
			},
		},
	}
	if compat {
		n2.Spec.CNMCompatDriverConfig = dc2
		n2.Spec.CNMCompatIPAM = ipam2
	} else {
		n2.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				DriverConfig: dc2,
				IPAM:         ipam2,
			},
		}
	}

	task1 := &api.Task{
		Networks: []*api.NetworkAttachment{
			{
				Network: n1,
			},
			{
				Network: n2,
			},
		},
	}

	task2 := &api.Task{
		Networks: []*api.NetworkAttachment{
			{
				Network: n1,
			},
			{
				Network: n2,
			},
		},
	}

	err := na1.Allocate(n1)
	assert.NoError(t, err)

	err = na1.Allocate(n2)
	assert.NoError(t, err)

	err = na1.AllocateTask(task1)
	assert.NoError(t, err)
	assert.Equal(t, len(task1.Networks[0].Addresses), 1)
	assert.Equal(t, len(task1.Networks[1].Addresses), 1)

	_, subnet1, _ := net.ParseCIDR("192.168.1.0/24")
	_, subnet2, _ := net.ParseCIDR("192.168.2.0/24")

	// variable coding: network/task/allocator
	ip111, _, err := net.ParseCIDR(task1.Networks[0].Addresses[0])
	assert.NoError(t, err)

	ip211, _, err := net.ParseCIDR(task1.Networks[1].Addresses[0])
	assert.NoError(t, err)

	assert.Equal(t, subnet1.Contains(ip111), true)
	assert.Equal(t, subnet2.Contains(ip211), true)

	err = na1.AllocateTask(task2)
	assert.NoError(t, err)
	assert.Equal(t, len(task2.Networks[0].Addresses), 1)
	assert.Equal(t, len(task2.Networks[1].Addresses), 1)

	ip121, _, err := net.ParseCIDR(task2.Networks[0].Addresses[0])
	assert.NoError(t, err)

	ip221, _, err := net.ParseCIDR(task2.Networks[1].Addresses[0])
	assert.NoError(t, err)

	assert.Equal(t, subnet1.Contains(ip121), true)
	assert.Equal(t, subnet2.Contains(ip221), true)

	// Now allocate the same the same tasks in a second allocator
	// but intentionally in reverse order.
	err = na2.Allocate(n1)
	assert.NoError(t, err)

	err = na2.Allocate(n2)
	assert.NoError(t, err)

	err = na2.AllocateTask(task2)
	assert.NoError(t, err)
	assert.Equal(t, len(task2.Networks[0].Addresses), 1)
	assert.Equal(t, len(task2.Networks[1].Addresses), 1)

	ip122, _, err := net.ParseCIDR(task2.Networks[0].Addresses[0])
	assert.NoError(t, err)

	ip222, _, err := net.ParseCIDR(task2.Networks[1].Addresses[0])
	assert.NoError(t, err)

	assert.Equal(t, subnet1.Contains(ip122), true)
	assert.Equal(t, subnet2.Contains(ip222), true)
	assert.Equal(t, ip121, ip122)
	assert.Equal(t, ip221, ip222)

	err = na2.AllocateTask(task1)
	assert.NoError(t, err)
	assert.Equal(t, len(task1.Networks[0].Addresses), 1)
	assert.Equal(t, len(task1.Networks[1].Addresses), 1)

	ip112, _, err := net.ParseCIDR(task1.Networks[0].Addresses[0])
	assert.NoError(t, err)

	ip212, _, err := net.ParseCIDR(task1.Networks[1].Addresses[0])
	assert.NoError(t, err)

	assert.Equal(t, subnet1.Contains(ip112), true)
	assert.Equal(t, subnet2.Contains(ip212), true)
	assert.Equal(t, ip111, ip112)
	assert.Equal(t, ip211, ip212)

	// Deallocate task
	err = na1.DeallocateTask(task1)
	assert.NoError(t, err)
	assert.Equal(t, len(task1.Networks[0].Addresses), 0)
	assert.Equal(t, len(task1.Networks[1].Addresses), 0)

	// Try allocation after free
	err = na1.AllocateTask(task1)
	assert.NoError(t, err)
	assert.Equal(t, len(task1.Networks[0].Addresses), 1)
	assert.Equal(t, len(task1.Networks[1].Addresses), 1)

	ip111, _, err = net.ParseCIDR(task1.Networks[0].Addresses[0])
	assert.NoError(t, err)

	ip211, _, err = net.ParseCIDR(task1.Networks[1].Addresses[0])
	assert.NoError(t, err)

	assert.Equal(t, subnet1.Contains(ip111), true)
	assert.Equal(t, subnet2.Contains(ip211), true)

	err = na1.DeallocateTask(task1)
	assert.NoError(t, err)
	assert.Equal(t, len(task1.Networks[0].Addresses), 0)
	assert.Equal(t, len(task1.Networks[1].Addresses), 0)

	// Try to free endpoints on an already freed task
	err = na1.DeallocateTask(task1)
	assert.NoError(t, err)
}
func TestAllocateTaskFree(t *testing.T) {
	testAllocateTaskFree(t, false)
}
func TestAllocateTaskFreeCompat(t *testing.T) {
	testAllocateTaskFree(t, true)
}

func testServiceAllocate(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)
	n := &api.Network{
		ID: "testID",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	if !compat {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{},
		}
	}

	s := &api.Service{
		ID: "testID1",
		Spec: api.ServiceSpec{
			Task: api.TaskSpec{
				Networks: []*api.NetworkAttachmentConfig{
					{
						Target: "testID",
					},
				},
			},
			Endpoint: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{
						Name:       "http",
						TargetPort: 80,
					},
					{
						Name:       "https",
						TargetPort: 443,
					},
				},
			},
		},
	}

	err := na.Allocate(n)
	assert.NoError(t, err)
	switch n.State.(type) {
	case *api.Network_CNM:
		assert.False(t, compat, "Compat CNM network has State.CMN")
	case nil:
		assert.True(t, compat, "CNM network has no State.CNM")
	default:
		assert.Fail(t, "Network has unexpected State")

	}
	cnmState := n.GetCNMCompat()
	assert.NotNil(t, cnmState)
	assert.NotEqual(t, cnmState.IPAM.Configs, nil)
	assert.Equal(t, len(cnmState.IPAM.Configs), 1)
	assert.Equal(t, cnmState.IPAM.Configs[0].Range, "")
	assert.Equal(t, len(cnmState.IPAM.Configs[0].Reserved), 0)

	_, subnet, err := net.ParseCIDR(cnmState.IPAM.Configs[0].Subnet)
	assert.NoError(t, err)

	gwip := net.ParseIP(cnmState.IPAM.Configs[0].Gateway)
	assert.NotEqual(t, gwip, nil)

	err = na.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(s.Endpoint.Ports))
	assert.True(t, s.Endpoint.Ports[0].PublishedPort >= dynamicPortStart &&
		s.Endpoint.Ports[0].PublishedPort <= dynamicPortEnd)
	assert.True(t, s.Endpoint.Ports[1].PublishedPort >= dynamicPortStart &&
		s.Endpoint.Ports[1].PublishedPort <= dynamicPortEnd)

	assert.Equal(t, 1, len(s.Endpoint.VirtualIPs))

	assert.Equal(t, s.Endpoint.Spec, s.Spec.Endpoint)

	ip, _, err := net.ParseCIDR(s.Endpoint.VirtualIPs[0].Addr)
	assert.NoError(t, err)

	assert.Equal(t, true, subnet.Contains(ip))
}
func TestServiceAllocate(t *testing.T) {
	testServiceAllocate(t, false)
}
func TestServiceAllocateCompat(t *testing.T) {
	testServiceAllocate(t, true)
}

func TestServiceAllocateUserDefinedPorts(t *testing.T) {
	na := newNetworkAllocator(t)
	s := &api.Service{
		ID: "testID1",
		Spec: api.ServiceSpec{
			Endpoint: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{
						Name:          "some_tcp",
						TargetPort:    1234,
						PublishedPort: 1234,
					},
					{
						Name:          "some_udp",
						TargetPort:    1234,
						PublishedPort: 1234,
						Protocol:      api.ProtocolUDP,
					},
				},
			},
		},
	}

	err := na.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(s.Endpoint.Ports))
	assert.Equal(t, uint32(1234), s.Endpoint.Ports[0].PublishedPort)
	assert.Equal(t, uint32(1234), s.Endpoint.Ports[1].PublishedPort)
}

func testServiceAllocateConflictingUserDefinedPorts(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)
	s := &api.Service{
		ID: "testID1",
		Spec: api.ServiceSpec{
			Endpoint: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{
						Name:          "some_tcp",
						TargetPort:    1234,
						PublishedPort: 1234,
					},
					{
						Name:          "some_other_tcp",
						TargetPort:    1234,
						PublishedPort: 1234,
					},
				},
			},
		},
	}

	err := na.ServiceAllocate(s)
	assert.Error(t, err)
}
func TestServiceAllocateConflictingUserDefinedPorts(t *testing.T) {
	testServiceAllocateConflictingUserDefinedPorts(t, false)
}
func TestServiceAllocateConflictingUserDefinedPortsCompat(t *testing.T) {
	testServiceAllocateConflictingUserDefinedPorts(t, true)
}

func TestServiceDeallocateAllocate(t *testing.T) {
	na := newNetworkAllocator(t)
	s := &api.Service{
		ID: "testID1",
		Spec: api.ServiceSpec{
			Endpoint: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{
						Name:          "some_tcp",
						TargetPort:    1234,
						PublishedPort: 1234,
					},
				},
			},
		},
	}

	err := na.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(s.Endpoint.Ports))
	assert.Equal(t, uint32(1234), s.Endpoint.Ports[0].PublishedPort)

	err = na.ServiceDeallocate(s)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(s.Endpoint.Ports))
	// Allocate again.
	err = na.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(s.Endpoint.Ports))
	assert.Equal(t, uint32(1234), s.Endpoint.Ports[0].PublishedPort)
}

func testServiceDeallocateAllocateIngressMode(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)

	n := &api.Network{
		ID: "testNetID1",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	if compat {
		n.Spec.CNMCompatIngress = true
	} else {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				Ingress: true,
			},
		}
	}

	err := na.Allocate(n)
	assert.NoError(t, err)

	s := &api.Service{
		ID: "testID1",
		Spec: api.ServiceSpec{
			Endpoint: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{
						Name:          "some_tcp",
						TargetPort:    1234,
						PublishedPort: 1234,
						PublishMode:   api.PublishModeIngress,
					},
				},
			},
		},
		Endpoint: &api.Endpoint{},
	}

	s.Endpoint.VirtualIPs = append(s.Endpoint.VirtualIPs,
		&api.Endpoint_VirtualIP{NetworkID: n.ID})

	err = na.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.Len(t, s.Endpoint.Ports, 1)
	assert.Equal(t, uint32(1234), s.Endpoint.Ports[0].PublishedPort)
	assert.Len(t, s.Endpoint.VirtualIPs, 1)

	err = na.ServiceDeallocate(s)
	assert.NoError(t, err)
	assert.Len(t, s.Endpoint.Ports, 0)
	assert.Len(t, s.Endpoint.VirtualIPs, 0)
	// Allocate again.
	s.Endpoint.VirtualIPs = append(s.Endpoint.VirtualIPs,
		&api.Endpoint_VirtualIP{NetworkID: n.ID})

	err = na.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.Len(t, s.Endpoint.Ports, 1)
	assert.Equal(t, uint32(1234), s.Endpoint.Ports[0].PublishedPort)
	assert.Len(t, s.Endpoint.VirtualIPs, 1)
}
func TestServiceDeallocateAllocateIngressMode(t *testing.T) {
	testServiceDeallocateAllocateIngressMode(t, false)
}
func TestServiceDeallocateAllocateIngressModeCompat(t *testing.T) {
	testServiceDeallocateAllocateIngressMode(t, true)
}

func testServiceAddRemovePortsIngressMode(t *testing.T, compat bool) {
	na := newNetworkAllocator(t)

	n := &api.Network{
		ID: "testNetID1",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	if compat {
		n.Spec.CNMCompatIngress = true
	} else {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				Ingress: true,
			},
		}
	}

	err := na.Allocate(n)
	assert.NoError(t, err)

	s := &api.Service{
		ID: "testID1",
		Spec: api.ServiceSpec{
			Endpoint: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{
						Name:          "some_tcp",
						TargetPort:    1234,
						PublishedPort: 1234,
						PublishMode:   api.PublishModeIngress,
					},
				},
			},
		},
		Endpoint: &api.Endpoint{},
	}

	s.Endpoint.VirtualIPs = append(s.Endpoint.VirtualIPs,
		&api.Endpoint_VirtualIP{NetworkID: n.ID})

	err = na.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.Len(t, s.Endpoint.Ports, 1)
	assert.Equal(t, uint32(1234), s.Endpoint.Ports[0].PublishedPort)
	assert.Len(t, s.Endpoint.VirtualIPs, 1)
	allocatedVIP := s.Endpoint.VirtualIPs[0].Addr

	//Unpublish port
	s.Spec.Endpoint.Ports = s.Spec.Endpoint.Ports[:0]
	err = na.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.Len(t, s.Endpoint.Ports, 0)
	assert.Len(t, s.Endpoint.VirtualIPs, 0)

	// Publish port again and ensure VIP is the same that was deallocated
	// and there is  no leak.
	s.Spec.Endpoint.Ports = append(s.Spec.Endpoint.Ports, &api.PortConfig{Name: "some_tcp",
		TargetPort:    1234,
		PublishedPort: 1234,
		PublishMode:   api.PublishModeIngress,
	})
	s.Endpoint.VirtualIPs = append(s.Endpoint.VirtualIPs,
		&api.Endpoint_VirtualIP{NetworkID: n.ID})
	err = na.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.Len(t, s.Endpoint.Ports, 1)
	assert.Equal(t, uint32(1234), s.Endpoint.Ports[0].PublishedPort)
	assert.Len(t, s.Endpoint.VirtualIPs, 1)
	assert.Equal(t, allocatedVIP, s.Endpoint.VirtualIPs[0].Addr)
}
func TestServiceAddRemovePortsIngressMode(t *testing.T) {
	testServiceAddRemovePortsIngressMode(t, false)
}
func TestServiceAddRemovePortsIngressModeCompat(t *testing.T) {
	testServiceAddRemovePortsIngressMode(t, true)
}

func TestServiceUpdate(t *testing.T) {
	na1 := newNetworkAllocator(t)
	na2 := newNetworkAllocator(t)
	s := &api.Service{
		ID: "testID1",
		Spec: api.ServiceSpec{
			Endpoint: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{
						Name:          "some_tcp",
						TargetPort:    1234,
						PublishedPort: 1234,
					},
					{
						Name:          "some_other_tcp",
						TargetPort:    1235,
						PublishedPort: 0,
					},
				},
			},
		},
	}

	err := na1.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.False(t, na1.ServiceNeedsAllocation(s))
	assert.Equal(t, 2, len(s.Endpoint.Ports))
	assert.Equal(t, uint32(1234), s.Endpoint.Ports[0].PublishedPort)
	assert.NotEqual(t, 0, s.Endpoint.Ports[1].PublishedPort)

	// Cache the secode node port
	allocatedPort := s.Endpoint.Ports[1].PublishedPort

	// Now allocate the same service in another allocator instance
	err = na2.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.False(t, na2.ServiceNeedsAllocation(s))
	assert.Equal(t, 2, len(s.Endpoint.Ports))
	assert.Equal(t, uint32(1234), s.Endpoint.Ports[0].PublishedPort)
	// Make sure we got the same port
	assert.Equal(t, allocatedPort, s.Endpoint.Ports[1].PublishedPort)

	s.Spec.Endpoint.Ports[1].PublishedPort = 1235
	assert.True(t, na1.ServiceNeedsAllocation(s))

	err = na1.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.False(t, na1.ServiceNeedsAllocation(s))
	assert.Equal(t, 2, len(s.Endpoint.Ports))
	assert.Equal(t, uint32(1234), s.Endpoint.Ports[0].PublishedPort)
	assert.Equal(t, uint32(1235), s.Endpoint.Ports[1].PublishedPort)
}

func TestServiceNetworkUpdate(t *testing.T) {
	na := newNetworkAllocator(t)

	n1 := &api.Network{
		ID: "testID1",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}

	n2 := &api.Network{
		ID: "testID2",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test2",
			},
		},
	}

	//Allocate both networks
	err := na.Allocate(n1)
	assert.NoError(t, err)

	err = na.Allocate(n2)
	assert.NoError(t, err)

	//Attach a network to a service spec nd allocate a service
	s := &api.Service{
		ID: "testID1",
		Spec: api.ServiceSpec{
			Task: api.TaskSpec{
				Networks: []*api.NetworkAttachmentConfig{
					{
						Target: "testID1",
					},
				},
			},
			Endpoint: &api.EndpointSpec{
				Mode: api.ResolutionModeVirtualIP,
			},
		},
	}

	err = na.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.False(t, na.ServiceNeedsAllocation(s))
	assert.Len(t, s.Endpoint.VirtualIPs, 1)

	// Now update the same service with another network
	s.Spec.Task.Networks = append(s.Spec.Task.Networks, &api.NetworkAttachmentConfig{Target: "testID2"})

	assert.True(t, na.ServiceNeedsAllocation(s))
	err = na.ServiceAllocate(s)
	assert.NoError(t, err)

	assert.False(t, na.ServiceNeedsAllocation(s))
	assert.Len(t, s.Endpoint.VirtualIPs, 2)

	s.Spec.Task.Networks = s.Spec.Task.Networks[:1]

	//Check if service needs update and allocate with updated service spec
	assert.True(t, na.ServiceNeedsAllocation(s))

	err = na.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.False(t, na.ServiceNeedsAllocation(s))
	assert.Len(t, s.Endpoint.VirtualIPs, 1)

	s.Spec.Task.Networks = s.Spec.Task.Networks[:0]
	//Check if service needs update with all the networks removed and allocate with updated service spec
	assert.True(t, na.ServiceNeedsAllocation(s))

	err = na.ServiceAllocate(s)
	assert.NoError(t, err)
	assert.False(t, na.ServiceNeedsAllocation(s))
	assert.Len(t, s.Endpoint.VirtualIPs, 0)

	//Attach a network and allocate service
	s.Spec.Task.Networks = append(s.Spec.Task.Networks, &api.NetworkAttachmentConfig{Target: "testID2"})
	assert.True(t, na.ServiceNeedsAllocation(s))

	err = na.ServiceAllocate(s)
	assert.NoError(t, err)

	assert.False(t, na.ServiceNeedsAllocation(s))
	assert.Len(t, s.Endpoint.VirtualIPs, 1)

}

type mockIpam struct {
	actualIpamOptions map[string]string
}

func (a *mockIpam) GetDefaultAddressSpaces() (string, string, error) {
	return "defaultAS", "defaultAS", nil
}

func (a *mockIpam) RequestPool(addressSpace, pool, subPool string, options map[string]string, v6 bool) (string, *net.IPNet, map[string]string, error) {
	a.actualIpamOptions = options

	poolCidr, _ := types.ParseCIDR(pool)
	return fmt.Sprintf("%s/%s", "defaultAS", pool), poolCidr, nil, nil
}

func (a *mockIpam) ReleasePool(poolID string) error {
	return nil
}

func (a *mockIpam) RequestAddress(poolID string, ip net.IP, opts map[string]string) (*net.IPNet, map[string]string, error) {
	return nil, nil, nil
}

func (a *mockIpam) ReleaseAddress(poolID string, ip net.IP) error {
	return nil
}

func (a *mockIpam) DiscoverNew(dType discoverapi.DiscoveryType, data interface{}) error {
	return nil
}

func (a *mockIpam) DiscoverDelete(dType discoverapi.DiscoveryType, data interface{}) error {
	return nil
}

func (a *mockIpam) IsBuiltIn() bool {
	return true
}

func testCorrectlyPassIPAMOptions(t *testing.T, compat bool) {
	var err error
	expectedIpamOptions := map[string]string{"network-name": "freddie"}

	na := newNetworkAllocator(t)
	ipamDriver := &mockIpam{}

	err = na.drvRegistry.RegisterIpamDriver("mockipam", ipamDriver)
	assert.NoError(t, err)

	n := &api.Network{
		ID: "testID",
		Spec: api.NetworkSpec{
			Annotations: api.Annotations{
				Name: "test",
			},
		},
	}
	dc := &api.Driver{}
	ipam := &api.IPAMOptions{
		Driver: &api.Driver{
			Name:    "mockipam",
			Options: expectedIpamOptions,
		},
		Configs: []*api.IPAMConfig{
			{
				Subnet:  "192.168.1.0/24",
				Gateway: "192.168.1.1",
			},
		},
	}
	if compat {
		n.Spec.CNMCompatDriverConfig = dc
		n.Spec.CNMCompatIPAM = ipam
	} else {
		n.Spec.Backend = &api.NetworkSpec_CNM{
			CNM: &api.CNMNetworkSpec{
				DriverConfig: dc,
				IPAM:         ipam,
			},
		}
	}
	err = na.Allocate(n)

	assert.Equal(t, expectedIpamOptions, ipamDriver.actualIpamOptions)
	assert.NoError(t, err)
}
func TestCorrectlyPassIPAMOptions(t *testing.T) {
	testCorrectlyPassIPAMOptions(t, false)
}
func TestCorrectlyPassIPAMOptionsCompat(t *testing.T) {
	testCorrectlyPassIPAMOptions(t, true)
}
