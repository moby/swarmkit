package volumeenforcer

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/state/store"
)

var _ = Describe("VolumeEnforcer", func() {
	var (
		s  *store.MemoryStore
		ve *VolumeEnforcer
	)

	BeforeEach(func() {
		s = store.NewMemoryStore(nil)
		ve = New(s)
	})

	Describe("rejectNoncompliantTasks", func() {
		var (
			n *api.Node
			v *api.Volume
			t *api.Task
		)

		BeforeEach(func() {
			// we don't, strictly speaking, need a node for this test, but we
			// might as well recreate the whole system rigging in case we
			// change things in the future
			n = &api.Node{
				ID: "node",
				Status: api.NodeStatus{
					State: api.NodeStatus_READY,
				},
			}

			v = &api.Volume{
				ID: "volumeID",
				Spec: api.VolumeSpec{
					Annotations: api.Annotations{
						Name: "volume",
					},
					Driver: &api.Driver{
						Name: "driver",
					},
					Availability: api.VolumeAvailabilityPause,
				},
				VolumeInfo: &api.VolumeInfo{
					VolumeID: "pluginID",
				},
				PublishStatus: []*api.VolumePublishStatus{
					{
						NodeID: "node",
						State:  api.VolumePublishStatus_PUBLISHED,
					},
				},
			}

			t = &api.Task{
				ID:     "task",
				NodeID: "node",
				Status: api.TaskStatus{
					State: api.TaskStateRunning,
				},
				DesiredState: api.TaskStateRunning,
				Volumes: []*api.VolumeAttachment{
					{
						ID:     "volumeID",
						Source: "foo",
						Target: "bar",
					},
				},
			}

			err := s.Update(func(tx store.Tx) error {
				if err := store.CreateNode(tx, n); err != nil {
					return err
				}
				if err := store.CreateVolume(tx, v); err != nil {
					return err
				}
				return store.CreateTask(tx, t)
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should skip volumes that do not have their availability as DRAIN", func() {
			ve.rejectNoncompliantTasks(v)

			var nt *api.Task
			s.View(func(tx store.ReadTx) {
				nt = store.GetTask(tx, t.ID)
			})

			Expect(nt).ToNot(BeNil())
			Expect(nt.Status.State).To(Equal(api.TaskStateRunning))
			Expect(nt.DesiredState).To(Equal(api.TaskStateRunning))
		})

		When("the Volume availability is DRAIN", func() {
			var (
				nv *api.Volume
			)

			BeforeEach(func() {
				err := s.Update(func(tx store.Tx) error {
					nv = store.GetVolume(tx, v.ID)
					nv.Spec.Availability = api.VolumeAvailabilityDrain
					return store.UpdateVolume(tx, nv)
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("should reject tasks belonging to a volume with availability DRAIN", func() {
				ve.rejectNoncompliantTasks(nv)

				var nt *api.Task
				s.View(func(tx store.ReadTx) {
					nt = store.GetTask(tx, t.ID)
				})
				Expect(nt).ToNot(BeNil())
				Expect(nt.Status.State).To(Equal(api.TaskStateRejected), "task state is %s", nt.Status.State)
				Expect(nt.DesiredState).To(Equal(api.TaskStateRunning), "task desired state is %s", nt.DesiredState)
			})

			It("should skip tasks that are already shut down", func() {
				err := s.Update(func(tx store.Tx) error {
					nt := store.GetTask(tx, t.ID)
					nt.Status.State = api.TaskStateCompleted
					return store.UpdateTask(tx, nt)
				})
				Expect(err).ToNot(HaveOccurred())

				ve.rejectNoncompliantTasks(nv)

				var nt *api.Task
				s.View(func(tx store.ReadTx) {
					nt = store.GetTask(tx, t.ID)
				})
				Expect(nt).ToNot(BeNil())
				Expect(nt.Status.State).To(Equal(api.TaskStateCompleted), "task state is %s", nt.Status.State)
				Expect(nt.DesiredState).To(Equal(api.TaskStateRunning), "task desired state is %s", nt.DesiredState)
			})
		})
	})
})
