package volumes

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/orchestrator/testutils"
	"github.com/docker/swarmkit/manager/state/store"
	stateutils "github.com/docker/swarmkit/manager/state/testutils"
)

var _ = Describe("Volumes", func() {
	var (
		ctx context.Context

		gclient *grpc.ClientConn
		client  csi.ControllerClient

		s       *store.MemoryStore
		cluster *api.Cluster

		stopMock func()

		vm                *VolumeManager
		stopVolumeManager <-chan struct{}
	)

	BeforeEach(func() {
		s = store.NewMemoryStore(&stateutils.MockProposer{})
		Expect(s).ToNot(BeNil())

		cluster = &api.Cluster{
			ID: "someID",
			Spec: api.ClusterSpec{
				Annotations: api.Annotations{
					Name: store.DefaultClusterName,
				},
			},
		}

		err := s.Update(func(tx store.Tx) error {
			return store.CreateCluster(tx, cluster)
		})
		Expect(err).ToNot(HaveOccurred())

		ctx = context.Background()
		gclient, stopMock = startMockServer(ctx, "driver1")
		client = csi.NewControllerClient(gclient)
		vm = NewVolumeManager(s)
	})

	JustBeforeEach(func() {
		stopVolumeManager = testutils.EnsureRuns(func() {
			vm.Run()
		})
	})

	AfterEach(func() {
		vm.Stop()
		Eventually(stopVolumeManager).Should(BeClosed())

		gclient.Close()
		gclient = nil
		client = nil
		stopMock()
	})

	When("Creating a new volume", func() {
		It("should call CreateVolume", func() {
			v := &api.Volume{
				ID: "someID",
				Spec: api.VolumeSpec{
					Annotations: api.Annotations{
						Name: "somevolume",
					},
					Driver: &api.Driver{
						Name: "driver1",
					},
				},
			}

			err := s.Update(func(tx store.Tx) error {
				return store.CreateVolume(tx, v)
			})
			Expect(err).ToNot(HaveOccurred())

			// TODO(dperny): run a pass of the volume manager
			cresp, err := client.CreateVolume(ctx, &csi.CreateVolumeRequest{
				Name: "somevolume",
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
						AccessType: &csi.VolumeCapability_Mount{
							Mount: &csi.VolumeCapability_MountVolume{},
						},
					},
				},
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(cresp).ToNot(BeNil())
			Expect(cresp.Volume).ToNot(BeNil())
			Expect(cresp.Volume.VolumeId).ToNot(BeEmpty())
			vid := cresp.Volume.VolumeId

			// fmt.Fprintf(GinkgoWriter, "volume created: %v\n", cresp.Volume)

			err = s.Update(func(tx store.Tx) error {
				tv := store.GetVolume(tx, v.ID)
				tv.VolumeID = vid
				return store.UpdateVolume(tx, tv)
			})

			// check if the volume has been updated to include a volume ID.
			// poll until this happens with Eventually. additionally, capture
			// the VolumeID in from the closure to use afterward
			var vid2 string
			Eventually(func() string {
				var v2 *api.Volume
				s.View(func(tx store.ReadTx) {
					v2 = store.GetVolume(tx, v.ID)
				})
				if v2 == nil {
					return ""
				}
				vid2 = v2.VolumeID
				return v2.VolumeID
			}).ShouldNot(BeEmpty())

			// check if the volume has been created. no need to poll, we
			// already have the volume ID.
			resp, err := client.ListVolumes(ctx, &csi.ListVolumesRequest{})
			Expect(err).ToNot(HaveOccurred())
			Expect(resp).ToNot(BeNil())
			Expect(resp.Entries).To(
				ContainElement(
					WithTransform(func(e *csi.ListVolumesResponse_Entry) string {
						if e != nil && e.Volume != nil {
							return e.Volume.VolumeId
						}
						return ""
					}, Equal(vid2)),
				),
			)
		})

		It("should select the correct CSI plugin", func() {
		})
	})
})
