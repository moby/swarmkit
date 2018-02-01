package scheduler

import (
	"time"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/genericresource"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func testRoleScheduler(t *testing.T) {
	ctx := context.Background()
	initialNodeSet := []*api.Node{
		{
			ID: "id1",
			Role: api.NodeRoleManager,
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Labels: map[string]string{
						"az": "az1",
					},
				},
			},
		},
		{
			ID: "id2",
			Role: api.NodeRoleManager,
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Labels: map[string]string{
						"az": "az1",
					},
				},
			},
		},
		{
			ID: "id3",
			Role: api.NodeRoleWorker,
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Labels: map[string]string{
						"az": "az1",
					},
				},
			},
		},
		{
			ID: "id4",
			Role: api.NodeRoleManager,
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Labels: map[string]string{
						"az": "az2",
					},
				},
			},
		},
		{
			ID: "id5",
			Role: api.NodeRoleWorker,
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Labels: map[string]string{
						"az": "az2",
					},
				},
			},
		},
		{
			ID: "id6",
			Role: api.NodeRoleWorker,
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Labels: map[string]string{
						"az": "az2",
					},
				},
			},
		},
	}

	serviceTemplate1 := &api.Service{
		Spec: &api.ServiceSpec{
			TaskTemplate: api.TaskSpec{
				Placement: &api.Placement{
					Preferences: []*api.PlacementPreference{
						{
							Preference: &api.PlacementPreference_Spread{
								Spread: &api.SpreadOver{
									SpreadDescriptor: "node.labels.az",
								},
							},
						},
					},
				},
			},
			Mode: api.ServiceMode{
				RoleManager: api.RoleManagerService{
					Replicas: 3,
				},
			},
		},
	}

	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	err := s.Update(func(tx store.Tx) error {
		// Prepoulate nodes
		for _, n := range initialNodeSet {
			assert.NoError(t, store.CreateNode(tx, n))
		}

		// Define service from template 1
		assert.NoError(t, store.CreateService(tx, serviceTemplate1))
		return nil
	})
	assert.NoError(t, err)

	scheduler := New(s)
	scheduler.nodeSet.alloc(len(initialNodeSet))

	for _, n := range initialNodeSet {
		s.nodeSet.addOrUpdateNode(newNodeInfo(n))
	}

	rs := newRoleScheduler(scheduler.ctx, scheduler.store, scheduler.nodeSet)

	go func() {
		assert.NoError(t, rs.Run(ctx))
	}()
	defer rs.cancel()

	//test init manager placement
	expectSpread := map[string]int{
		az1: 2,
		az2: 1,
	}
	spreadCount := map[string]int{
		az1: 0,
		az2: 0,
	}
	for _, active := range rs.managers.active {
		azSpread[active.Spec.Annotations.Labels.az]++
	}

	assert.Equal(t, rs.activeManagers(), rs.specifiedManagers())
	assert.Equal(t, spreadCount, expectSpread)

	// test a failed manager
	failManager := *nodeInfo
	for _, m := range rs.managers.active {
		if m.Spec.Annotations.Labels.az == az2 {
			failManager = m
			break
		}
	}
	failManager.Status.State = NodeStatus_DOWN
	failManagerUpdater := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateNode(tx, failManager))
		return nil
	})
	assert.NoError(t, failManagerUpdater)
	for _, pending := range rs.managers.pending {
		assert.Equal(t, pending.Spec.Annotations.Labels.az, az2)
	}
	assert.Equal(t, rs.managers.failed[failManager.ID], failManager)
	assert.Equal(t, rs.scheduledManagers(), rs.specifiedManagers())

	// test service definition update
	updateService := rs.currentService
	updateService.Spec.Mode.RoleManager.Replicas = 5
	serviceUpdater := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.UpdateService(tx, updateService))
		return nil
	})
	assert.NoError(t, serviceUpdater)
	assert.Equal(t, rs.scheduledManagers(), rs.specifiedManagers())

}
