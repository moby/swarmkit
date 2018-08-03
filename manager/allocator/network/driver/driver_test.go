package driver

import (
	. "github.com/onsi/ginkgo/extensions/table"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/docker/swarmkit/api"
)

var _ = Describe("driver package", func() {
	DescribeTable("driver.IsAllocated", func(n *api.Network, expected bool) {
		Expect(n).To(WithTransform(IsAllocated, Equal(expected)))
	},
		Entry("nil driver", &api.Network{
			DriverState: nil,
		}, false),
		Entry("empty driver name", &api.Network{
			DriverState: &api.Driver{Name: ""},
		}, false),
		Entry("empty spec", &api.Network{
			DriverState: &api.Driver{Name: "overlay"},
			Spec:        api.NetworkSpec{DriverConfig: &api.Driver{Name: ""}},
		}, true),
		Entry("matching spec", &api.Network{
			DriverState: &api.Driver{Name: "fooName"},
			Spec:        api.NetworkSpec{DriverConfig: &api.Driver{Name: "fooName"}},
		}, true),
		Entry("nil spec", &api.Network{
			DriverState: &api.Driver{Name: "overlay"},
		}, true),
	)
})
