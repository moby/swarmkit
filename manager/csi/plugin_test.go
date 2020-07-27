package csi

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"context"

	// "google.golang.org/grpc"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/docker/swarmkit/api"
)

// newPluginFromClients creates a new plugin using the provided CSI RPC
// clients.
func newPluginFromClients(name string, provider SecretProvider, idClient csi.IdentityClient, controllerClient csi.ControllerClient) *plugin {
	return &plugin{
		name:             name,
		provider:         provider,
		idClient:         idClient,
		controllerClient: controllerClient,
	}
}

const driverName = "testdriver"

var _ = Describe("Plugin manager", func() {
	var (
		plugin   *plugin
		provider *fakeSecretProvider

		controller *fakeControllerClient
		idClient   *fakeIdentityClient

		// gclient  *grpc.ClientConn
		// stopMock func()
	)

	BeforeEach(func() {
		provider = &fakeSecretProvider{
			secretMap: map[string]*api.Secret{
				"secretID1": {
					ID: "secretID1",
					Spec: api.SecretSpec{
						Data: []byte("superdupersecret1"),
					},
				},
				"secretID2": {
					ID: "secretID2",
					Spec: api.SecretSpec{
						Data: []byte("superdupersecret2"),
					},
				},
			},
		}

		controller = newFakeControllerClient()
		idClient = newFakeIdentityClient()

		plugin = newPluginFromClients(driverName, provider, idClient, controller)
	})

	JustBeforeEach(func() {
		err := plugin.init(context.Background())
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("creating a volume", func() {
		var (
			v          *api.Volume
			volumeInfo *api.VolumeInfo
			err        error
		)

		BeforeEach(func() {
			v = &api.Volume{
				ID: "someID",
				Spec: api.VolumeSpec{
					Annotations: api.Annotations{
						Name: "someVolume",
					},
					Driver: &api.Driver{
						Name: driverName,
						Options: map[string]string{
							"param1": "val1",
							"param2": "val2",
						},
					},
					AccessMode: &api.VolumeAccessMode{
						Scope:   api.VolumeScopeMultiNode,
						Sharing: api.VolumeSharingOneWriter,
					},
					Secrets: []*api.VolumeSecret{
						{
							Key:    "password1",
							Secret: "secretID1",
						},
						{
							Key:    "password2",
							Secret: "secretID2",
						},
					},
					AccessibilityRequirements: &api.TopologyRequirement{
						Requisite: []*api.Topology{
							{
								Segments: map[string]string{
									"region": "R1",
									"zone":   "Z1",
								},
							},
						},
					},
					CapacityRange: &api.CapacityRange{
						RequiredBytes: 1000000000,
						LimitBytes:    1000000000,
					},
				},
			}

		})

		JustBeforeEach(func() {
			volumeInfo, err = plugin.CreateVolume(context.Background(), v)
		})

		It("should return a correct VolumeInfo object", func() {
			Expect(volumeInfo).ToNot(BeNil())
			Expect(volumeInfo.CapacityBytes).To(Equal(int64(1000000000)))
			Expect(volumeInfo.VolumeContext).To(SatisfyAll(
				HaveLen(2),
				HaveKeyWithValue("someFlag", "yeah"),
				HaveKeyWithValue("requestNumber", "1"),
			))
			Expect(volumeInfo.VolumeID).To(Equal("volumeid1"))
		})

		It("should not return an error", func() {
			Expect(err).ToNot(HaveOccurred())
		})

		It("should create correct volume requests", func() {
			Expect(controller.createVolumeRequests).To(HaveLen(1))
			createVolumeRequest := controller.createVolumeRequests[0]

			Expect(createVolumeRequest).ToNot(BeNil())

			Expect(createVolumeRequest.Name).To(Equal(v.Spec.Annotations.Name))
			Expect(createVolumeRequest.Parameters).To(Equal(v.Spec.Driver.Options))
			Expect(createVolumeRequest.VolumeCapabilities).To(Equal([]*csi.VolumeCapability{
				{
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER,
					},
				},
			}))

			Expect(createVolumeRequest.Secrets).To(SatisfyAll(
				HaveLen(2),
				HaveKeyWithValue("password1", "superdupersecret1"),
				HaveKeyWithValue("password2", "superdupersecret2"),
			))

			Expect(createVolumeRequest.AccessibilityRequirements).To(Equal(
				&csi.TopologyRequirement{
					Requisite: []*csi.Topology{
						{
							Segments: map[string]string{
								"region": "R1",
								"zone":   "Z1",
							},
						},
					},
				},
			))
			Expect(createVolumeRequest.CapacityRange).To(Equal(
				&csi.CapacityRange{
					RequiredBytes: 1000000000,
					LimitBytes:    1000000000,
				},
			))
		})

		When("the plugin exposes no ControllerService", func() {
			BeforeEach(func() {
				idClient.caps = []*csi.PluginCapability{}
			})

			It("should not call CreateVolume", func() {
				Expect(controller.createVolumeRequests).To(HaveLen(0))
			})

			It("should not return an error", func() {
				Expect(err).ToNot(HaveOccurred())
			})

			It("should return the volume name as the VolumeID", func() {
				Expect(volumeInfo.VolumeID).To(Equal(v.Spec.Annotations.Name))
			})
		})
	})
})
