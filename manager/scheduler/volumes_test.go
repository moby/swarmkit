package scheduler

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/state/store"
)

// cannedVolume is a quick helper method that creates canned volumes, using the
// integer ID argument to differentiate them.
func cannedVolume(id int) *api.Volume {
	return &api.Volume{
		Id: fmt.Sprintf("volumeID%d", id),
		Spec: &api.VolumeSpec{
			Annotations: &api.Annotations{
				Name: fmt.Sprintf("volume%d", id),
			},
			Group: "group",
			Driver: &api.Driver{
				Name: "driver",
			},
			AccessMode: &api.VolumeAccessMode{
				Scope:   api.VolumeAccessMode_MULTI_NODE,
				Sharing: api.VolumeAccessMode_ALL,
			},
		},
		VolumeInfo: &api.VolumeInfo{
			VolumeId: fmt.Sprintf("volumePlugin%d", id),
		},
	}
}

var _ = Describe("volumeSet", func() {
	var (
		vs *volumeSet
	)

	BeforeEach(func() {
		vs = newVolumeSet()
	})

	Describe("adding or removing volumes", func() {
		var (
			v1, v2 *api.Volume
		)

		BeforeEach(func() {
			v1 = cannedVolume(1)
			v2 = cannedVolume(2)

			vs.addOrUpdateVolume(v1)
			vs.addOrUpdateVolume(v2)
		})

		It("should keep track of added volumes", func() {
			Expect(vs.volumes).To(SatisfyAll(
				HaveKeyWithValue(
					v1.Id,
					volumeInfo{volume: v1, tasks: map[string]volumeUsage{}, nodes: map[string]int{}},
				),
				HaveKeyWithValue(
					v2.Id,
					volumeInfo{volume: v2, tasks: map[string]volumeUsage{}, nodes: map[string]int{}},
				),
			))

			// cleaner lines by assigning empty struct here
			z := struct{}{}
			Expect(vs.byGroup).To(
				HaveKeyWithValue(
					"group", map[string]struct{}{v1.Id: z, v2.Id: z},
				),
			)
			Expect(vs.byName).To(SatisfyAll(
				HaveKeyWithValue(v1.GetSpec().GetAnnotations().GetName(), v1.Id),
				HaveKeyWithValue(v2.GetSpec().GetAnnotations().GetName(), v2.Id),
			))
		})

		It("should remove volumes fully", func() {
			vs.removeVolume(v1.Id)

			Expect(vs.volumes).To(SatisfyAll(
				HaveLen(1),
				// don't need to check the value.
				HaveKey(v2.Id),
			))
			Expect(vs.byName).To(SatisfyAll(
				HaveLen(1),
				HaveKeyWithValue(v2.GetSpec().GetAnnotations().GetName(), v2.Id),
			))
			// if the volume is the last one in the group, it should be removed.
			Expect(vs.byGroup).To(SatisfyAll(
				HaveLen(1),
				HaveKeyWithValue("group", map[string]struct{}{v2.Id: {}}),
			))
		})

		It("should track tasks using a volume", func() {
			vs.reserveVolume(v1.Id, "task1", "node1", true)
			Expect(vs.volumes[v1.Id].tasks).To(SatisfyAll(
				HaveLen(1),
				HaveKeyWithValue("task1", volumeUsage{nodeID: "node1", readOnly: true}),
			))
		})

		It("should reserve all task volumes", func() {
			task := &api.Task{
				Id:     "task1",
				NodeId: "node1",
				Volumes: []*api.VolumeAttachment{
					{
						Id:     v1.Id,
						Source: v1.Spec.GetGroup(),
						Target: "/var/spool/mail",
					}, {
						Id:     v2.Id,
						Source: v2.GetSpec().GetAnnotations().GetName(),
						Target: "/srv/www",
					},
				},
				Spec: &api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Mounts: []*api.Mount{
								{
									Type:   api.Mount_CLUSTER,
									Source: v1.Spec.GetGroup(),
									Target: "/var/spool/mail",
								}, {
									Type:   api.Mount_BIND,
									Source: "/var/run/docker.sock",
									Target: "/var/run/docker.sock",
								}, {
									Type:     api.Mount_CLUSTER,
									Source:   v2.GetSpec().GetAnnotations().GetName(),
									Target:   "/srv/www",
									Readonly: true,
								},
							},
						},
					},
				},
			}

			vs.reserveTaskVolumes(task)

			Expect(vs.volumes[v1.Id].tasks).To(SatisfyAll(
				HaveLen(1),
				HaveKeyWithValue("task1", volumeUsage{nodeID: "node1", readOnly: false}),
			))
			Expect(vs.volumes[v2.Id].tasks).To(SatisfyAll(
				HaveLen(1),
				HaveKeyWithValue("task1", volumeUsage{nodeID: "node1", readOnly: true}),
			))
		})
	})

	Describe("volume availability on a node", func() {
		// this test case uses a table to efficiently cover all of the cases.
		// it starts by defining some helper types, and then uses
		// "DescribeTable", which is a ginkgo extension for writing
		// table-driven tests quickly.

		// UsedBy is an enum that allows the test case to specify whether the
		// volume is in use and how
		type UsedBy int
		const (
			// Unused is default, and indicatest that the volume is not
			// presently in use.
			Unused UsedBy = iota
			// WrongNode indicates that the volume should be marked in use by
			// a node other than the one being checked
			WrongNode
			// OnlyReaders indicates that the volume is in use, but all other
			// users are read-only.
			OnlyReaders
			// Writer indicates that the volume is in use by another writer.
			Writer
		)

		// AvailabilityCase encapsulates the table parameters for the test
		type AvailabilityCase struct {
			// AccessMode is the access mode the volume should have
			AccessMode *api.VolumeAccessMode
			// InUse is the existing use state of the volume.
			InUse UsedBy
			// InTopology indicates whether the volume should lie within the
			// node's topology
			InTopology bool
			// ReadOnly indicates whether the requested use of the volume is
			// read-only
			ReadOnly bool
			// Expected is the expected result.
			Expected bool
		}

		// this table test uses the most basic set of parameters to cover all of
		// the functionality of checkVolume. It sets up a default node and volume,
		// filled in with default fields. Those fields are modified based on the
		// parameters of the availability case.
		DescribeTable("checkVolume",
			func(c AvailabilityCase) {
				// if there is no access mode specified, set a default
				if c.AccessMode == nil {
					c.AccessMode = &api.VolumeAccessMode{
						Scope:   api.VolumeAccessMode_SINGLE_NODE,
						Sharing: api.VolumeAccessMode_ALL,
					}
				}

				v := &api.Volume{
					Id: "someVolume",
					Spec: &api.VolumeSpec{
						AccessMode: c.AccessMode,
						Driver: &api.Driver{
							Name: "somePlugin",
						},
					},
					VolumeInfo: &api.VolumeInfo{
						VolumeId: "somePluginVolumeID",
						AccessibleTopology: []*api.Topology{
							{Segments: map[string]string{"zone": "z1"}},
						},
					},
				}
				vs.addOrUpdateVolume(v)

				var top *api.Topology
				if c.InTopology {
					top = &api.Topology{
						Segments: map[string]string{"zone": "z1"},
					}
				} else {
					top = &api.Topology{
						Segments: map[string]string{"zone": "z2"},
					}
				}

				n := &api.Node{
					Id: "someNode",
					Description: &api.NodeDescription{
						CsiInfo: []*api.NodeCSIInfo{
							{
								PluginName:         "somePlugin",
								AccessibleTopology: top,
							},
						},
					},
				}

				ni := newNodeInfo(n, nil, &api.Resources{})

				// if the volume is unused, do nothing.
				switch c.InUse {
				case WrongNode:
					vs.reserveVolume(v.Id, "someTask", "someOtherNode", false)
				case OnlyReaders:
					vs.reserveVolume(v.Id, "someTask", "someNode", true)
				case Writer:
					vs.reserveVolume(v.Id, "someTask", "someNode", true)
					vs.reserveVolume(v.Id, "someWriter", "someNode", false)
				}

				available := vs.checkVolume(v.Id, &ni, c.ReadOnly)
				Expect(available).To(Equal(c.Expected))
			},
			Entry("volume outside of node topology", AvailabilityCase{
				// we don't need to explicitly set this, but it makes the
				// test case more clear
				InTopology: false,
			}),
			Entry("volume in use on a different node", AvailabilityCase{
				InTopology: true,
				InUse:      WrongNode,
			}),
			Entry("volume is read only, mount is not", AvailabilityCase{
				AccessMode: &api.VolumeAccessMode{
					Scope:   api.VolumeAccessMode_MULTI_NODE,
					Sharing: api.VolumeAccessMode_READ_ONLY,
				},
				InTopology: true,
				ReadOnly:   false,
			}),
			Entry("volume is OneWriter, but already has a writer", AvailabilityCase{
				AccessMode: &api.VolumeAccessMode{
					Scope:   api.VolumeAccessMode_MULTI_NODE,
					Sharing: api.VolumeAccessMode_ONE_WRITER,
				},
				InTopology: true,
				ReadOnly:   false,
				InUse:      Writer,
			}),
			Entry("volume is OneWriter, and has no writer", AvailabilityCase{
				AccessMode: &api.VolumeAccessMode{
					Scope:   api.VolumeAccessMode_MULTI_NODE,
					Sharing: api.VolumeAccessMode_ONE_WRITER,
				},
				InTopology: true,
				InUse:      OnlyReaders,
				ReadOnly:   false,
				Expected:   true,
			}),
			Entry("volume not in use and is in topology", AvailabilityCase{
				InTopology: true,
				Expected:   true,
			}),
			Entry("the volume is in use on a different node, but the scope is multinode", AvailabilityCase{
				AccessMode: &api.VolumeAccessMode{
					Scope:   api.VolumeAccessMode_MULTI_NODE,
					Sharing: api.VolumeAccessMode_ALL,
				},
				InTopology: true,
				InUse:      WrongNode,
				Expected:   true,
			}),
			Entry("the volume is in use and cannot be shared", AvailabilityCase{
				AccessMode: &api.VolumeAccessMode{
					Scope:   api.VolumeAccessMode_SINGLE_NODE,
					Sharing: api.VolumeAccessMode_NONE,
				},
				InTopology: true,
				InUse:      OnlyReaders,
				ReadOnly:   true,
				Expected:   false,
			}),
			Entry("the volume is not in use and cannot be shared", AvailabilityCase{
				AccessMode: &api.VolumeAccessMode{
					Scope:   api.VolumeAccessMode_SINGLE_NODE,
					Sharing: api.VolumeAccessMode_NONE,
				},
				InTopology: true,
				InUse:      Unused,
				ReadOnly:   true,
				Expected:   true,
			}),
		)

		Describe("volume or group availability on a node", func() {
			var (
				node    *NodeInfo
				volumes []*api.Volume
			)

			BeforeEach(func() {
				n := &api.Node{
					Id: "someNode",
					Description: &api.NodeDescription{
						CsiInfo: []*api.NodeCSIInfo{
							{
								PluginName: "newPlugin",
								NodeId:     "newPluginSomeNode",
								// don't bother with topology for these tests.
							},
						},
					},
				}
				ni := newNodeInfo(n, nil, &api.Resources{})
				node = &ni

				volumes = []*api.Volume{
					{
						Id: "volume1",
						Spec: &api.VolumeSpec{
							Annotations: &api.Annotations{
								Name: "volumeName1",
							},
							Driver: &api.Driver{
								Name: "newPlugin",
							},
							AccessMode: &api.VolumeAccessMode{
								Scope:   api.VolumeAccessMode_SINGLE_NODE,
								Sharing: api.VolumeAccessMode_ALL,
							},
						},
						VolumeInfo: &api.VolumeInfo{
							VolumeId: "newPluginVolume1",
						},
					},
					{
						Id: "volume2",
						Spec: &api.VolumeSpec{
							Annotations: &api.Annotations{
								Name: "volumeName2",
							},
							Driver: &api.Driver{
								Name: "newPlugin",
							},
							AccessMode: &api.VolumeAccessMode{
								Scope:   api.VolumeAccessMode_SINGLE_NODE,
								Sharing: api.VolumeAccessMode_ALL,
							},
						},
					},
					{
						Id: "volume3",
						Spec: &api.VolumeSpec{
							Annotations: &api.Annotations{
								Name: "volumeName3",
							},
							Driver: &api.Driver{
								Name: "newPlugin",
							},
							AccessMode: &api.VolumeAccessMode{
								Scope:   api.VolumeAccessMode_SINGLE_NODE,
								Sharing: api.VolumeAccessMode_ALL,
							},
							Group: "someVolumeGroup",
						},
						VolumeInfo: &api.VolumeInfo{
							VolumeId: "newPluginVolume3",
						},
					},
					{
						Id: "volume4",
						Spec: &api.VolumeSpec{
							Annotations: &api.Annotations{
								Name: "volumeName4",
							},
							Driver: &api.Driver{
								Name: "newPlugin",
							},
							AccessMode: &api.VolumeAccessMode{
								Scope:   api.VolumeAccessMode_SINGLE_NODE,
								Sharing: api.VolumeAccessMode_ALL,
							},
							Group: "someVolumeGroup",
						},
						VolumeInfo: &api.VolumeInfo{
							VolumeId: "newPluginVolume4",
						},
					},
				}

				for _, v := range volumes {
					vs.addOrUpdateVolume(v)
				}
			})

			It("should choose and return an available volume", func() {
				mount := &api.Mount{
					Type:   api.Mount_CLUSTER,
					Source: "volumeName1",
				}
				volumeID := vs.isVolumeAvailableOnNode(mount, node)
				Expect(volumeID).To(Equal("volume1"))
			})

			It("should return an empty string if there are no available volumes", func() {
				mount := &api.Mount{
					Type:   api.Mount_CLUSTER,
					Source: "volumeNameNotReal",
				}
				volumeID := vs.isVolumeAvailableOnNode(mount, node)
				Expect(volumeID).To(BeEmpty())
			})

			It("should specify one volume from a group, if the source is a group", func() {
				mount := &api.Mount{
					Type:   api.Mount_CLUSTER,
					Source: "group:someVolumeGroup",
				}
				volumeID := vs.isVolumeAvailableOnNode(mount, node)
				Expect(volumeID).To(Or(Equal("volume3"), Equal("volume4")))
			})
		})

		It("should choose task volumes", func() {
			v1 := cannedVolume(1)
			v1.Spec.Group = "volumeGroup"
			vs.addOrUpdateVolume(v1)
			v2 := cannedVolume(2)
			vs.addOrUpdateVolume(v2)
			v3 := cannedVolume(3)
			vs.addOrUpdateVolume(v3)
			mounts := []*api.Mount{
				{
					Type:     api.Mount_CLUSTER,
					Source:   "group:volumeGroup",
					Target:   "/somedir",
					Readonly: true,
				}, {
					Type:   api.Mount_CLUSTER,
					Source: v2.GetSpec().GetAnnotations().GetName(),
					Target: "/someOtherDir",
				}, {
					Type:   api.Mount_BIND,
					Source: "/some/subdir",
					Target: "/some/container/dir",
				}, {
					Type:   api.Mount_CLUSTER,
					Source: v3.GetSpec().GetAnnotations().GetName(),
					Target: "/some/third/dir",
				},
			}

			task := &api.Task{
				Id: "taskID1",
				Spec: &api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Mounts: mounts,
						},
					},
				},
			}

			node := &NodeInfo{
				Node: &api.Node{
					Id:          "node1",
					Description: &api.NodeDescription{},
				},
			}

			attachments, err := vs.chooseTaskVolumes(task, node)
			Expect(err).ToNot(HaveOccurred())
			Expect(attachments).To(ConsistOf(
				&api.VolumeAttachment{Id: v1.Id, Source: mounts[0].Source, Target: mounts[0].Target},
				&api.VolumeAttachment{Id: v2.Id, Source: mounts[1].Source, Target: mounts[1].Target},
				&api.VolumeAttachment{Id: v3.Id, Source: mounts[3].Source, Target: mounts[3].Target},
			))
		})
	})

	Describe("freeVolumes", func() {
		var (
			s *store.MemoryStore
			// allVolume is used by every task on every node.
			allVolume *api.Volume
			volumes   []*api.Volume
			nodes     []*api.Node
			tasks     []*api.Task
		)

		BeforeEach(func() {
			s = store.NewMemoryStore(nil)
			volumes = nil
			nodes = nil
			tasks = nil

			allVolume = cannedVolume(5)

			// add some volumes
			for i := range 4 {
				volumes = append(volumes, cannedVolume(i))
				vs.addOrUpdateVolume(volumes[i])

				nodes = append(nodes, &api.Node{
					Id: fmt.Sprintf("node%d", i),
				})

				volumes[i].PublishStatus = []*api.VolumePublishStatus{
					{
						NodeId: nodes[i].Id,
						State:  api.VolumePublishStatus_PUBLISHED,
					},
				}

				tasks = append(tasks, &api.Task{
					Id:     fmt.Sprintf("task%d", i),
					NodeId: nodes[i].Id,
					Spec: &api.TaskSpec{
						Runtime: &api.TaskSpec_Container{
							Container: &api.ContainerSpec{
								Mounts: []*api.Mount{
									{
										Type:   api.Mount_CLUSTER,
										Source: volumes[i].GetSpec().GetAnnotations().GetName(),
										Target: "bar",
									},
									{
										Type:   api.Mount_CLUSTER,
										Source: allVolume.GetSpec().GetAnnotations().GetName(),
										Target: "baz",
									},
								},
							},
						},
					},
					Volumes: []*api.VolumeAttachment{
						{
							Source: volumes[i].GetSpec().GetAnnotations().GetName(),
							Target: "bar",
							Id:     volumes[i].Id,
						}, {
							Source: allVolume.GetSpec().GetAnnotations().GetName(),
							Target: "baz",
							Id:     allVolume.Id,
						},
					},
				})
				allVolume.PublishStatus = append(allVolume.PublishStatus, &api.VolumePublishStatus{
					NodeId: nodes[i].Id,
					State:  api.VolumePublishStatus_PUBLISHED,
				})
			}
			err := s.Update(func(tx store.Tx) error {
				for _, v := range volumes {
					if err := store.CreateVolume(tx, v); err != nil {
						return err
					}
					vs.addOrUpdateVolume(v)
				}

				if err := store.CreateVolume(tx, allVolume); err != nil {
					return err
				}
				vs.addOrUpdateVolume(allVolume)

				for _, n := range nodes {
					if err := store.CreateNode(tx, n); err != nil {
						return err
					}
				}

				for _, t := range tasks {
					if err := store.CreateTask(tx, t); err != nil {
						return err
					}
					vs.reserveTaskVolumes(t)
				}
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should have nodes reference counted correctly", func() {
			for _, v := range volumes {
				info := vs.volumes[v.Id]

				Expect(info.nodes).To(HaveLen(1))
				Expect(info.volume.PublishStatus).ToNot(BeNil())
				nid := info.volume.PublishStatus[0].NodeId
				Expect(nid).ToNot(BeEmpty())
				Expect(info.nodes[nid]).To(Equal(1))
			}

			allInfo := vs.volumes[allVolume.Id]
			Expect(allInfo.nodes).To(HaveLen(4))
			Expect(allInfo.volume.PublishStatus).ToNot(BeNil())
			for _, status := range allInfo.volume.PublishStatus {
				Expect(allInfo.nodes[status.NodeId]).To(Equal(1))
			}
		})

		It("should free volumes that are no longer needed", func() {
			vs.releaseVolume(volumes[0].Id, tasks[0].Id)
			vs.releaseVolume(allVolume.Id, tasks[0].Id)

			err := s.Batch(vs.freeVolumes)
			Expect(err).ToNot(HaveOccurred())

			var freshVolumes []*api.Volume
			s.View(func(tx store.ReadTx) {
				freshVolumes, _ = store.FindVolumes(tx, store.All)
			})
			Expect(freshVolumes).To(HaveLen(5))

			for _, v := range freshVolumes {
				switch v.Id {
				case volumes[0].Id:
					Expect(v.PublishStatus).To(ConsistOf(&api.VolumePublishStatus{
						State:  api.VolumePublishStatus_PENDING_NODE_UNPUBLISH,
						NodeId: nodes[0].Id,
					}))
				case allVolume.Id:
					for _, status := range v.PublishStatus {
						if status.NodeId == nodes[0].Id {
							Expect(status.State).To(Equal(api.VolumePublishStatus_PENDING_NODE_UNPUBLISH))
						} else {
							Expect(status.State).To(Equal(api.VolumePublishStatus_PUBLISHED))
						}
					}
				default:
					Expect(v.PublishStatus).To(HaveLen(1))
					Expect(v.PublishStatus[0].State).To(Equal(api.VolumePublishStatus_PUBLISHED))
				}
			}
		})
	})
})
