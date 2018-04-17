package allocator

// TODO(dperny): rename this file to allocator_test.go

import (
	// testing imports
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/types"

	// standard libraries
	"context"
	"fmt"
	"reflect"
	"time"

	// the packages under in this integration test
	// imported for PortsMostlyEqual
	"github.com/docker/swarmkit/manager/allocator/network/port"

	// external libraries
	"github.com/docker/libnetwork/ipamapi"
	"github.com/sirupsen/logrus"

	// our libraries
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state/store"
)

const (
	// allocStopTimeout defines the time we expect the allocator to have
	// stopped by
	allocStopTimeout = 10 * time.Second

	// ingressID is the ID of the ingress network, which will be present for
	// every test unless it is explicitly removed
	ingressID = "ingressNetID"
)

// This suite is an integration test of the allocator, which uses real, live
// components. It should be a definitive test of the allocator's public API. If
// these tests pass, then any changes to the allocator should not require
// changes to other components that depend on it.
var _ = Describe("allocator.NewAllocator", func() {
	var (
		ctx       context.Context
		dataStore *store.MemoryStore
		allocator *NewAllocator

		// allocatorExitResult is the result of the running allocator exiting
		// from its go routine. If this channel does not write either nil or
		// an error, then the allocator did not close.
		allocatorExitResult chan error
	)

	// Before we start the tests, we need to create a store and an allocator.
	// We won't start the allocator until just before the tests, so that the
	// subspecs can add data to it before initialization
	BeforeEach(func() {
		// create a custom logger such that it won't spew info unless the test
		// fails. GinkgoWriter is what does this for us.
		logger := logrus.New()
		logger.Out = GinkgoWriter
		logger.Level = logrus.DebugLevel
		ctx = log.WithLogger(context.Background(), logrus.NewEntry(logger))

		// store has no proposer, because it's not in a cluster
		dataStore = store.NewMemoryStore(nil)

		// the allocator has no PluginGetter.
		// TODO(dperny): we should handle this case, somehow, and provide a
		// PluginGetter so the integration tests can run with it.
		allocator = NewNew(dataStore, nil)

		// reinitialize this channel every spec
		allocatorExitResult = make(chan error)

		// create an ingress network, which we desire to exist by default in
		// every test
		ingress := &api.Network{
			ID: ingressID,
			Spec: api.NetworkSpec{
				Annotations: api.Annotations{
					Name: ingressID,
				},
				// setting ingress true is the only thing that needs to be
				// done. the rest of the initialization can happen in the
				// allocator using the defaults.
				Ingress: true,
			},
		}
		err := dataStore.Update(func(tx store.Tx) error {
			return store.CreateNetwork(tx, ingress)
		})
		Expect(err).ToNot(HaveOccurred())
	})

	// Just before we start the tests, we should start the allocator
	JustBeforeEach(func() {
		go func() {
			err := allocator.Run(ctx)
			allocatorExitResult <- err
		}()
	})

	// After each test, we need to cancel the context and verify that the
	// allocator fully and correctly stops
	AfterEach(func() {
		timeout := time.After(allocStopTimeout)
		allocator.Stop()
		var err error
		select {
		case err = <-allocatorExitResult:
		case <-timeout:
			err = fmt.Errorf("allocator did not stop in %v seconds", allocStopTimeout)
		}

		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Updating a service", func() {
		// This tests that a service can be updated to add and remove networks
		// successfully.
		var (
			// use only IDs to keep track of objects; we will get them from the
			// store when we need them.
			serviceID = "testUpdateService"
			taskID    = "testUpdateTask"
			nw1       = "testNet1"
			nw2       = "testNet2"
			nw3       = "testNet3"
		)

		BeforeEach(func() {
			err := dataStore.Update(func(tx store.Tx) error {
				// all we need is an ID, let the allocator fill in the rest
				err := store.CreateNetwork(tx, &api.Network{
					ID: nw1,
					Spec: api.NetworkSpec{
						Annotations: api.Annotations{Name: nw1},
					},
				})
				Expect(err).ToNot(HaveOccurred())

				err = store.CreateNetwork(tx, &api.Network{
					ID: nw2,
					Spec: api.NetworkSpec{
						Annotations: api.Annotations{Name: nw2},
					},
				})
				Expect(err).ToNot(HaveOccurred())

				err = store.CreateNetwork(tx, &api.Network{
					ID: nw3,
					Spec: api.NetworkSpec{
						Annotations: api.Annotations{Name: nw3},
					},
				})
				Expect(err).ToNot(HaveOccurred())

				service := &api.Service{
					ID: serviceID,
					Spec: api.ServiceSpec{
						Task: api.TaskSpec{
							Networks: []*api.NetworkAttachmentConfig{
								{Target: nw1},
								{Target: nw2},
							},
						},
					},
					SpecVersion: &api.Version{},
				}

				err = store.CreateService(tx, service)
				Expect(err).ToNot(HaveOccurred())

				// we only really need 1 task for this test
				err = store.CreateTask(tx, &api.Task{
					ID:           taskID,
					ServiceID:    serviceID,
					Spec:         service.Spec.Task,
					DesiredState: api.TaskStateRunning,
				})
				Expect(err).ToNot(HaveOccurred())

				return nil
			})

			Expect(err).ToNot(HaveOccurred())
		})

		// Before we do this test, verify that everything succeesfully
		// allocates
		JustBeforeEach(func() {
			// there are a lot of Eventually calls here. these calls poll the
			// enclosed function and check the matcher against their result
			Eventually(func() *api.Network {
				var nw *api.Network
				dataStore.View(func(tx store.ReadTx) {
					nw = store.GetNetwork(tx, nw1)
				})
				return nw
			}, 3*time.Second, 500*time.Millisecond).Should(BeAllocated())

			Eventually(func() *api.Network {
				var nw *api.Network
				dataStore.View(func(tx store.ReadTx) {
					nw = store.GetNetwork(tx, nw2)
				})
				return nw
			}, 3*time.Second, 500*time.Millisecond).Should(BeAllocated())

			Eventually(func() *api.Network {
				var nw *api.Network
				dataStore.View(func(tx store.ReadTx) {
					nw = store.GetNetwork(tx, nw3)
				})
				return nw
			}, 3*time.Second, 500*time.Millisecond).Should(BeAllocated())

			Eventually(func() *api.Service {
				var service *api.Service
				dataStore.View(func(tx store.ReadTx) {
					service = store.GetService(tx, serviceID)
				})
				return service
			}, 3*time.Second, 500*time.Millisecond).Should(BeAllocated())

			Eventually(func() *api.Task {
				var task *api.Task
				dataStore.View(func(tx store.ReadTx) {
					task = store.GetTask(tx, taskID)
				})
				return task
			}, 3*time.Second, 500*time.Millisecond).Should(BeAllocated())
		})

		It("should successfully update the service", func() {
			// now update the service
			var service *api.Service
			err := dataStore.Update(func(tx store.Tx) error {
				service = store.GetService(tx, serviceID)
				Expect(service).ToNot(BeNil())

				// update the service
				// slice out just the first of the two network
				service.Spec.Task.Networks = service.Spec.Task.Networks[0:1]
				service.SpecVersion.Index++
				err := store.UpdateService(tx, service)
				Expect(err).ToNot(HaveOccurred())
				return nil
			})
			Expect(err).ToNot(HaveOccurred())

			newTaskID := taskID + "New"
			// shut down the old task and start the new task
			err = dataStore.Update(func(tx store.Tx) error {
				task := store.GetTask(tx, taskID)
				Expect(task).ToNot(BeNil())

				task.DesiredState = api.TaskStateShutdown
				task.Status.State = api.TaskStateShutdown

				// create a new task
				newTask := &api.Task{
					ID:           newTaskID,
					ServiceID:    serviceID,
					DesiredState: api.TaskStateRunning,
					Spec:         service.Spec.Task,
				}
				err := store.UpdateTask(tx, task)
				Expect(err).ToNot(HaveOccurred())

				err = store.CreateTask(tx, newTask)
				Expect(err).ToNot(HaveOccurred())
				return nil
			})
			Expect(err).ToNot(HaveOccurred())

			Eventually(func() *api.Task {
				var t *api.Task
				dataStore.View(func(tx store.ReadTx) {
					t = store.GetTask(tx, newTaskID)
				})
				return t
			}).Should(BeAllocated())

			Eventually(func() *api.Service {
				var s *api.Service
				dataStore.View(func(tx store.ReadTx) {
					s = store.GetService(tx, serviceID)
				})
				return s
			}).Should(BeAllocated())
		})
	})
})

// No tests below this point, only helper code.

// allocatedMatcher is a custom matcher type to verify that an object is fully
// and correctly allocated according to its spec.
type allocatedMatcher struct {
	msg error
}

// BeAllocated is a custom matcher that verifies that a swarmkit object is
// fully and correctly allocated. In its current state, it checks most of the
// cases, but does not verify corner cases.
func BeAllocated() GomegaMatcher {
	return &allocatedMatcher{}
}

func (a *allocatedMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected object to be in fully allocated state\n\tobject: %v\n\terror: %v", actual, a.msg)
}

func (a *allocatedMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected object to not be in fully allocated state")
}

func (a *allocatedMatcher) Match(actual interface{}) (bool, error) {
	var (
		success bool
		msg     error
	)
	switch a := actual.(type) {
	case *api.Network:
		success, msg = networkAllocated(a)
	case *api.Service:
		success, msg = serviceAllocated(a)
	case *api.Task:
		success, msg = taskAllocated(a)
	case *api.Node:
		// TODO(dperny): checking if a node is fully allocated requires having
		// the network states.
		return false, fmt.Errorf("node allocation check not supported")
	default:
		return false, fmt.Errorf("object is not allocatable")
	}
	if !success {
		a.msg = msg
	}
	return success, nil
}

func networkAllocated(n *api.Network) (bool, error) {
	// check that the driver state exists
	if n.DriverState == nil {
		return false, fmt.Errorf("network driver is nil")
	}
	if n.DriverState.Name == "" {
		return false, fmt.Errorf("network driver name is empty")
	}

	// if a driver config exists and has a non-empty name, check that it
	// matches the driver state name.
	if n.Spec.DriverConfig != nil && n.Spec.DriverConfig.Name != "" {
		if n.DriverState.Name != n.Spec.DriverConfig.Name {
			return false, fmt.Errorf(
				"network driver does not match. actual: %v expected: %v",
				n.DriverState.Name, n.Spec.DriverConfig.Name,
			)
			// TODO(dperny): not checking driver options because i'm not sure
			// if they necessarily SHOULD always match
		}
	} else {
		// otherwise, verify that it matches the default
		if n.DriverState.Name != "overlay" {
			return false, fmt.Errorf("with no driver name specified, expected the default of overlay")
		}
	}

	// check the IPAM state is valid

	// first, does the IPAM exist
	if n.IPAM == nil {
		return false, fmt.Errorf("IPAM state is nil")
	}
	// check the ipam driver exists
	if n.IPAM.Driver == nil {
		return false, fmt.Errorf("IPAM driver is nil")
	}
	// verify that the driver name is not empty
	if n.IPAM.Driver.Name == "" {
		return false, fmt.Errorf("IPAM driver name is empty")
	}

	// now, check against the spec, if one exists, or the defaults otherwise
	if n.Spec.IPAM != nil {
		// if there is a driver specified, similar check to above
		if n.Spec.IPAM.Driver != nil && n.Spec.IPAM.Driver.Name != "" {
			if n.IPAM.Driver.Name != n.Spec.IPAM.Driver.Name {
				return false, fmt.Errorf(
					"ipam driver does not match. actual: %v spec: %v",
					n.IPAM.Driver.Name, n.Spec.IPAM.Driver.Name,
				)
			}
			// if there are any IPAM configs defined, check that they match the
			// actual ones used.
			if len(n.Spec.IPAM.Configs) != 0 {
				if len(n.IPAM.Configs) != len(n.Spec.IPAM.Configs) {
					return false, fmt.Errorf(
						"ipam configs do not match. actual: %v spec: %v",
						n.IPAM.Configs, n.Spec.IPAM.Configs,
					)
				}
				// check that each IPAM config in the spec a matching one
				// in the object
				// TODO(dperny): this test is WIP, so we'll check the full
				// configs for equality later
			} else {
				// if there are no IPAM configs specified, check that we have
				// exactly one config
				if len(n.IPAM.Configs) != 1 {
					return false, fmt.Errorf(
						"with no IPAM configs defined, expected 1 default but got %v",
						n.IPAM.Configs,
					)
				}
			}
		} else {
			// check that the driver name is the default
			if n.DriverState.Name != ipamapi.DefaultIPAM {
				return false, fmt.Errorf(
					"with no ipam driver specified, expected the default of %v",
					ipamapi.DefaultIPAM,
				)
			}
		}
	} else {
		// otherwise, check against the defaults
		if n.IPAM.Driver.Name != ipamapi.DefaultIPAM {
			return false, fmt.Errorf(
				"with no ipam driver specified, expected the default of %v",
				ipamapi.DefaultIPAM,
			)
		}
		if len(n.IPAM.Configs) != 1 {
			return false, fmt.Errorf("expected 1 default ipam config, got %v", n.IPAM.Configs)
		}
	}

	return true, nil
}

func serviceAllocated(s *api.Service) (bool, error) {
	// check that the service endpoint exists
	if s.Endpoint == nil {
		return false, fmt.Errorf("service endpoint is not allocated")
	}

	// while we're checking port list equivalency, we'll also check if the
	// service needs the ingress network.
	needsIngress := false

	// if the Spec.Endpoint is nil, we should check the Endpoint.Spec against
	// an empty spec. Otherwise, if it is not nil, we should check the specs
	// against each other, and then validate the ports as well
	if s.Spec.Endpoint == nil {
		if len(s.Endpoint.Spec.Ports) != 0 {
			return false, fmt.Errorf(
				"nil spec on the object should give empty spec in the endpoint, actually: %v",
				s.Endpoint.Spec,
			)
		}
		if s.Endpoint.Spec.Mode != api.ResolutionModeVirtualIP {
			return false, fmt.Errorf(
				"nil spec on the object should give default mode in the endpoint, actually: %v",
				s.Endpoint.Spec.Mode,
			)
		}
	} else {
		// check that the Spec.Endpoint matches the Endpoint.Spec
		if !reflect.DeepEqual(s.Endpoint.Spec, s.Spec.Endpoint) {
			return false, fmt.Errorf(
				"spec do not match: Endpoint.Spec: %v Spec.Endpoint: %v",
				s.Endpoint.Spec, s.Spec.Endpoint,
			)
		}
		// check if the service exposes ports
		if len(s.Spec.Endpoint.Ports) != len(s.Endpoint.Ports) {
			return false, fmt.Errorf(
				"expected spec ports to match endpoint ports. actual: %v spec: %v",
				s.Endpoint.Ports, s.Spec.Endpoint.Ports,
			)
		}

		// NOTE(dperny): this is not able to detect the case where two dynamically
		// allocated published ports share the same target port... not sure if that
		// is even a valid possibility
		for _, p := range s.Endpoint.Ports {
			portFound := false
			for _, config := range s.Spec.Endpoint.Ports {
				// if the ports are almost the same, make sure that the published
				// ports are valid
				if port.PortsMostlyEqual(p, config) {
					// if the port is in host mode, make sure published ports are
					// the same, because we do not dynamically allocate
					if config.PublishMode == api.PublishModeHost {
						if p.PublishedPort != config.PublishedPort {
							return false, fmt.Errorf(
								"expected spec ports to match endpoint ports. actual: %v spec: %v",
								s.Endpoint.Ports, s.Spec.Endpoint.Ports,
							)
						}
					} else {
						needsIngress = true
						if config.PublishedPort == 0 && p.PublishedPort == 0 {
							return false, fmt.Errorf("expected port with published port 0 to have port dynamically assigned")
						}
						if config.PublishedPort != p.PublishedPort {
							return false, fmt.Errorf(
								"expected spec ports to match endpoint ports. actual: %v spec: %v",
								s.Endpoint.Ports, s.Spec.Endpoint.Ports,
							)
						}
					}
					portFound = true
					break
				}
			}
			if !portFound {
				return false, fmt.Errorf(
					"expected spec ports to match endpoint ports. actual: %v spec: %v",
					s.Endpoint.Ports, s.Spec.Endpoint.Ports,
				)
			}
		}

	}

	// check that the endpoint has one vip for every network attachment
	networksExpected := map[string]struct{}{}
	for _, network := range s.Spec.Task.Networks {
		networksExpected[network.Target] = struct{}{}
	}
	if needsIngress {
		networksExpected[ingressID] = struct{}{}
	}

	// go through all of the vips, and if we find a vip, delete it from the
	// expected map
	for _, vip := range s.Endpoint.VirtualIPs {
		if _, ok := networksExpected[vip.NetworkID]; !ok {
			return false, fmt.Errorf(
				"found vip in object that is not in spec. VIPs: %v, expected: %v",
				s.Endpoint.VirtualIPs, networksExpected,
			)
		}
		delete(networksExpected, vip.NetworkID)
	}

	// if we still have networks in the map, that means we don't have VIPs for
	// them
	if len(networksExpected) != 0 {
		return false, fmt.Errorf(
			"did not find VIP for some networks in service, VIPs: %v Missing: %v",
			s.Endpoint.VirtualIPs, networksExpected,
		)
	}

	return true, nil
}

func taskAllocated(t *api.Task) (bool, error) {
	// verify the task is in the PENDING state, and the message is correct
	// NOTE(dperny): we are not expecting tasks to progress past the PENDING
	// state
	if t.Status.State != api.TaskStatePending || t.Status.Message != AllocatedStatusMessage {
		return false, fmt.Errorf("task is not in pending state with correct message: %v", t.Status)
	}

	// if the endpoint is nil, we're not allocated yet
	if t.Endpoint == nil {
		return false, fmt.Errorf("task endpoint is nil")
	}

	ingressNeeded := false
	// check if ingress is needed
	for _, p := range t.Endpoint.Ports {
		if p.PublishMode == api.PublishModeHost {
			ingressNeeded = true
			break
		}
	}

	networksNeeded := len(t.Spec.Networks)
	if ingressNeeded {
		networksNeeded = networksNeeded + 1
	}

	if len(t.Networks) != networksNeeded {
		return false, fmt.Errorf(
			"number of networks on task does not match number of networks needed (ingress: %v), have: %v, need %v",
			ingressNeeded, t.Networks, t.Spec.Networks,
		)
	}

	// NOTE(dperny): this does not solve the case of two attachments to the
	// same network
	for _, n := range t.Networks {
		// we'll verify that the attachment is allocated after, but if it's not
		// the ingress attachment, we need to make sure it belongs
		if n.Network == nil {
			return false, fmt.Errorf("task attachment has a nil network, attachments: %v", t.Networks)
		}

		if n.Network.ID != ingressID {
			configFound := true
			for _, spec := range t.Spec.Networks {
				if n.Network.ID == spec.Target {
					configFound = true
					break
				}
			}
			if !configFound {
				return false, fmt.Errorf(
					"network attachments do not match spec. actual: %v, spec: %v",
					t.Networks, t.Spec.Networks,
				)
			}
		}
		// verify that there is one address is present on the attachment
		if len(n.Addresses) != 1 {
			return false, fmt.Errorf("network attachment does not have 1 address: %v", n)
		}
	}

	// if all this checks ou t, then we're good to go.
	return true, nil
}
