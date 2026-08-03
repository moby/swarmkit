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
				Id: "node",
				Status: &api.NodeStatus{
					State: api.NodeStatus_READY,
				},
			}

			v = &api.Volume{
				Id: "volumeID",
				Spec: &api.VolumeSpec{
					Annotations: &api.Annotations{
						Name: "volume",
					},
					Driver: &api.Driver{
						Name: "driver",
					},
					Availability: api.VolumeSpec_PAUSE,
				},
				VolumeInfo: &api.VolumeInfo{
					VolumeId: "pluginID",
				},
				PublishStatus: []*api.VolumePublishStatus{
					{
						NodeId: "node",
						State:  api.VolumePublishStatus_PUBLISHED,
					},
				},
			}

			t = &api.Task{
				Id:     "task",
				NodeId: "node",
				Status: &api.TaskStatus{
					State: api.TaskState_RUNNING,
				},
				DesiredState: api.TaskState_RUNNING,
				Volumes: []*api.VolumeAttachment{
					{
						Id:     "volumeID",
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
				nt = store.GetTask(tx, t.Id)
			})

			Expect(nt).ToNot(BeNil())
			Expect(nt.Status.GetState()).To(Equal(api.TaskState_RUNNING))
			Expect(nt.DesiredState).To(Equal(api.TaskState_RUNNING))
		})

		When("the Volume availability is DRAIN", func() {
			var (
				nv *api.Volume
			)

			BeforeEach(func() {
				err := s.Update(func(tx store.Tx) error {
					nv = store.GetVolume(tx, v.Id)
					nv.Spec.Availability = api.VolumeSpec_DRAIN
					return store.UpdateVolume(tx, nv)
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("should reject tasks belonging to a volume with availability DRAIN", func() {
				ve.rejectNoncompliantTasks(nv)

				var nt *api.Task
				s.View(func(tx store.ReadTx) {
					nt = store.GetTask(tx, t.Id)
				})
				Expect(nt).ToNot(BeNil())
				Expect(nt.Status.GetState()).To(Equal(api.TaskState_REJECTED), "task state is %s", nt.Status.GetState())
				Expect(nt.DesiredState).To(Equal(api.TaskState_RUNNING), "task desired state is %s", nt.DesiredState)
			})

			It("should skip tasks that are already shut down", func() {
				err := s.Update(func(tx store.Tx) error {
					nt := store.GetTask(tx, t.Id)
					nt.Status.State = api.TaskState_COMPLETE
					return store.UpdateTask(tx, nt)
				})
				Expect(err).ToNot(HaveOccurred())

				ve.rejectNoncompliantTasks(nv)

				var nt *api.Task
				s.View(func(tx store.ReadTx) {
					nt = store.GetTask(tx, t.Id)
				})
				Expect(nt).ToNot(BeNil())
				Expect(nt.Status.GetState()).To(Equal(api.TaskState_COMPLETE), "task state is %s", nt.Status.GetState())
				Expect(nt.DesiredState).To(Equal(api.TaskState_RUNNING), "task desired state is %s", nt.DesiredState)
			})
		})
	})
})
