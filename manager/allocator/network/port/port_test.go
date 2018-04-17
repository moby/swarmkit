package port_test

import (
	. "github.com/docker/swarmkit/manager/allocator/network/port"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"fmt"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/allocator/network/errors"
)

var _ = Describe("port.Allocator", func() {
	var (
		pa Allocator
	)

	BeforeEach(func() {
		pa = NewAllocator()
	})

	Describe("adding a new endpoint", func() {
		Context("to a clean port allocator", func() {
			var (
				spec, specCopy *api.EndpointSpec
				endpoint       *api.Endpoint
				p              Proposal
				err            error
			)
			BeforeEach(func() {
				spec = &api.EndpointSpec{
					Ports: []*api.PortConfig{
						{
							Name:          "http",
							Protocol:      api.ProtocolTCP,
							TargetPort:    80,
							PublishedPort: 80,
							PublishMode:   api.PublishModeIngress,
						},
						{
							Name:          "https",
							Protocol:      api.ProtocolTCP,
							TargetPort:    443,
							PublishedPort: 443,
							PublishMode:   api.PublishModeIngress,
						},
					},
				}
				endpoint = &api.Endpoint{}
				specCopy = spec.Copy()
				p, err = pa.Allocate(endpoint, spec)
			})
			It("should succeed", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(p).NotTo(BeNil())
				Expect(p.IsNoop()).To(BeFalse())
			})
			It("should not modify the spec", func() {
				Expect(spec).To(Equal(specCopy))
			})
			It("should not modify the endpoint", func() {
				e := &api.Endpoint{}
				Expect(endpoint.Spec).To(Equal(e.Spec))
				Expect(endpoint.Ports).To(BeEmpty())
			})
			It("should not modify the Allocator state before calling Commit", func() {
				// calling Allocate a second time will succeed because no state
				// was committed
				_, err := pa.Allocate(&api.Endpoint{}, spec)
				Expect(err).ToNot(HaveOccurred())
			})
			It("should be idempotent", func() {
				// grab a copy of the endpoint so we have something concrete to
				// compare to
				endpointCopy := endpoint.Copy()
				endpointCopy.Spec = spec
				endpointCopy.Ports = p.Ports()
				p.Commit()
				// run this a few times and make sure that it works
				for i := 0; i < 5; i++ {
					// shadow these variables
					p, err := pa.Allocate(endpointCopy, spec)
					Expect(err).ToNot(HaveOccurred())
					Expect(p).ToNot(BeNil())
					Expect(p.Ports()).To(ConsistOf(endpointCopy.Ports))
					Expect(p.IsNoop()).To(BeTrue())
					p.Commit()
				}
			})
		})

		Context("to an allocator with ports in use", func() {
			BeforeEach(func() {
				ports := []*api.PortConfig{
					{
						Name:          "http",
						Protocol:      api.ProtocolTCP,
						TargetPort:    80,
						PublishedPort: 80,
						PublishMode:   api.PublishModeIngress,
					},
					{
						Name:          "https",
						Protocol:      api.ProtocolTCP,
						TargetPort:    443,
						PublishedPort: 443,
						PublishMode:   api.PublishModeIngress,
					},
				}
				endpoints := []*api.Endpoint{
					{
						Spec: &api.EndpointSpec{
							Ports: ports,
						},
						Ports: ports,
					},
				}
				pa.Restore(endpoints)
			})

			Context("with colliding ports in the new endpoint", func() {
				var (
					spec     *api.EndpointSpec
					endpoint *api.Endpoint
					p        Proposal
					err      error
				)
				BeforeEach(func() {
					spec = &api.EndpointSpec{
						Ports: []*api.PortConfig{
							{
								Name:          "https",
								Protocol:      api.ProtocolTCP,
								TargetPort:    443,
								PublishedPort: 443,
								PublishMode:   api.PublishModeIngress,
							},
						},
					}
					endpoint = &api.Endpoint{}
					p, err = pa.Allocate(endpoint, spec)
				})

				It("should return ErrResourceInUse", func() {
					Expect(err).To(HaveOccurred())
					Expect(errors.IsErrResourceInUse(err)).To(BeTrue())
					Expect(p).To(BeNil())
					Expect(err.Error()).To(Equal("port 443/TCP is in use"))
				})
			})

			Context("with no colliding ports in the new endpoint", func() {
				var (
					endpoint *api.Endpoint
					spec     *api.EndpointSpec
					p        Proposal
					err      error
				)
				BeforeEach(func() {
					spec = &api.EndpointSpec{
						Ports: []*api.PortConfig{
							{
								Name:          "httpalt",
								Protocol:      api.ProtocolTCP,
								TargetPort:    80,
								PublishedPort: 8080,
								PublishMode:   api.PublishModeIngress,
							},
						},
					}
					endpoint = &api.Endpoint{}
					p, err = pa.Allocate(endpoint, spec)
				})

				It("should succeed", func() {
					Expect(err).ToNot(HaveOccurred())
					Expect(p).ToNot(BeNil())
				})
				It("should return ports matching the spec", func() {
					Expect(p.Ports()).To(ConsistOf(spec.Ports))
				})
			})

			Context("with colliding port numbers, but different protocols", func() {
				var (
					endpoint *api.Endpoint
					spec     *api.EndpointSpec
					p        Proposal
					err      error
				)
				BeforeEach(func() {
					spec = &api.EndpointSpec{
						Ports: []*api.PortConfig{
							{
								Name:          "http",
								Protocol:      api.ProtocolUDP,
								TargetPort:    80,
								PublishedPort: 80,
								PublishMode:   api.PublishModeIngress,
							},
						},
					}
					endpoint = &api.Endpoint{}
					p, err = pa.Allocate(endpoint, spec)
				})

				It("should succeed", func() {
					Expect(err).ToNot(HaveOccurred())
					Expect(p).ToNot(BeNil())
				})
				It("should return ports matching the spec", func() {
					Expect(p.Ports()).To(ConsistOf(spec.Ports))
				})
			})
		})

		Context("with invalid publish or target ports", func() {
			It("should return ErrInvalidSpec", func() {
				spec := &api.EndpointSpec{
					Ports: []*api.PortConfig{
						{
							PublishedPort: 66666,
						},
					},
				}
				_, err := pa.Allocate(&api.Endpoint{}, spec)
				Expect(err).To(HaveOccurred())
				Expect(err).To(WithTransform(
					func(e error) bool { return errors.IsErrInvalidSpec(e) },
					BeTrue(),
				))
				Expect(err.Error()).To(Equal(
					"spec is invalid: published port 66666 isn't in the valid port range",
				))

				spec.Ports[0].TargetPort = 66666
				spec.Ports[0].PublishedPort = 0
				_, err = pa.Allocate(&api.Endpoint{}, spec)
				Expect(err).To(HaveOccurred())
				Expect(err).To(WithTransform(
					func(e error) bool { return errors.IsErrInvalidSpec(e) },
					BeTrue(),
				))
				Expect(err.Error()).To(Equal(
					"spec is invalid: target port 66666 isn't in the valid port range",
				))
			})
		})
	})

	Describe("updating an endpoint", func() {
		var endpoint *api.Endpoint
		BeforeEach(func() {
			ports := []*api.PortConfig{
				{
					Name:          "http",
					Protocol:      api.ProtocolTCP,
					TargetPort:    80,
					PublishedPort: 80,
					PublishMode:   api.PublishModeIngress,
				},
				{
					Name:          "https",
					Protocol:      api.ProtocolTCP,
					TargetPort:    443,
					PublishedPort: 443,
					PublishMode:   api.PublishModeIngress,
				},
			}
			endpoint = &api.Endpoint{
				Spec: &api.EndpointSpec{
					Ports: ports,
				},
				Ports: ports,
			}
			pa.Restore([]*api.Endpoint{endpoint})
		})

		Context("to add ports", func() {
			Context("if ports collide", func() {
				var (
					spec *api.EndpointSpec
					p    Proposal
					err  error
				)
				BeforeEach(func() {
					spec = endpoint.Spec.Copy()
					spec.Ports = append(spec.Ports, &api.PortConfig{
						Name:          "http2",
						Protocol:      api.ProtocolTCP,
						PublishedPort: 80,
						TargetPort:    8080,
						PublishMode:   api.PublishModeIngress,
					})
					p, err = pa.Allocate(endpoint, spec)
				})
				It("should fail with ErrInvalidSpec", func() {
					Expect(err).To(HaveOccurred())
					Expect(errors.IsErrInvalidSpec(err)).To(BeTrue())
					Expect(err.Error()).To(Equal(
						"spec is invalid: published port 80/TCP is assigned to more than 1 port config",
					))
					Expect(p).To(BeNil())
				})
			})
			Context("if ports do not collide", func() {
				var (
					endpointCopy *api.Endpoint
					p            Proposal
					err          error
				)
				BeforeEach(func() {
					endpointCopy = endpoint.Copy()
					spec := endpoint.Spec.Copy()
					spec.Ports = append(spec.Ports, &api.PortConfig{
						Name:          "http2",
						Protocol:      api.ProtocolTCP,
						PublishedPort: 8080,
						TargetPort:    8080,
						PublishMode:   api.PublishModeIngress,
					})
					p, err = pa.Allocate(endpointCopy, spec)
				})
				It("should succeed", func() {
					Expect(err).ToNot(HaveOccurred())
					Expect(p).ToNot(BeNil())
				})
				It("should add return ports matching the spec's ports", func() {
					Expect(p.Ports()).To(ConsistOf(
						append(endpoint.Ports, &api.PortConfig{
							Name:          "http2",
							Protocol:      api.ProtocolTCP,
							PublishedPort: 8080,
							TargetPort:    8080,
							PublishMode:   api.PublishModeIngress,
						}),
					))
				})
			})
		})

		Context("to remove ports", func() {
			var (
				endpointCopy *api.Endpoint
				spec         *api.EndpointSpec
				p            Proposal
				err          error
			)
			BeforeEach(func() {
				endpointCopy = endpoint.Copy()
				spec = endpoint.Spec.Copy()
				// remove the last ports
				spec.Ports = spec.Ports[:1]
				p, err = pa.Allocate(endpointCopy, spec)
			})
			It("should succeed", func() {
				Expect(err).ToNot(HaveOccurred())
				Expect(p).ToNot(BeNil())
			})
			It("should return ports matching the spec's ports", func() {
				Expect(p.Ports()).To(ConsistOf(spec.Ports))
				Expect(len(p.Ports())).To(Equal(len(endpoint.Ports) - 1))
			})
			It("should free the removed ports for another object to use", func() {
				// commit so we can do another allocation
				p.Commit()

				newSpec := spec.Copy()
				newSpec.Ports = newSpec.Ports[1:]
				newEndpoint := &api.Endpoint{}
				// shadowing p and err here
				p, err := pa.Allocate(newEndpoint, newSpec)
				Expect(err).ToNot(HaveOccurred())
				Expect(p).ToNot(BeNil())
			})
			It("should not free any ports not removed", func() {
				// commit so we can do another allocation
				p.Commit()

				newSpec := spec.Copy()
				newSpec.Ports = newSpec.Ports[:1]
				newEndpoint := &api.Endpoint{}
				// shadowing p and err here
				p, err := pa.Allocate(newEndpoint, newSpec)
				Expect(err).To(HaveOccurred())
				Expect(errors.IsErrResourceInUse(err)).To(BeTrue())
				Expect(p).To(BeNil())
			})
		})
		Context("to change name and target port", func() {
			It("should be a noop", func() {
				spec := endpoint.Spec.Copy()
				spec.Ports[0].TargetPort = 999
				spec.Ports[0].Name = "something else"
				p, err := pa.Allocate(endpoint, spec)
				Expect(err).ToNot(HaveOccurred())
				Expect(p.IsNoop()).To(BeTrue())
			})
			It("should return ports matching the spec's ports", func() {
				spec := endpoint.Spec.Copy()
				spec.Ports[0].TargetPort = uint32(999)
				spec.Ports[0].Name = "something else"
				p, _ := pa.Allocate(endpoint, spec)

				Expect(p.Ports()[0].TargetPort).To(Equal(uint32(999)))
				Expect(p.Ports()[0].Name).To(Equal("something else"))
			})
		})
		Context("to change published port", func() {
			var (
				endpointCopy *api.Endpoint
				p            Proposal
				err          error
			)
			BeforeEach(func() {
				endpointCopy = endpoint.Copy()
				spec := endpoint.Spec.Copy()
				spec.Ports[0].PublishedPort = uint32(999)
				p, err = pa.Allocate(endpointCopy, spec)
			})
			It("should allocate the new port", func() {
				Expect(err).ToNot(HaveOccurred())
				Expect(p).ToNot(BeNil())
				Expect(p.IsNoop()).To(BeFalse())
				Expect(p.Ports()[0].PublishedPort).To(Equal(uint32(999)))

				p.Commit()

				// try allocating the port we just allocated
				_, err := pa.Allocate(&api.Endpoint{}, &api.EndpointSpec{
					Ports: []*api.PortConfig{
						p.Ports()[0],
					},
				})
				Expect(err).To(HaveOccurred())
				Expect(errors.IsErrResourceInUse(err)).To(BeTrue())
			})
			It("should free the old port", func() {
				p.Commit()

				q, err := pa.Allocate(&api.Endpoint{}, &api.EndpointSpec{
					Ports: []*api.PortConfig{
						{
							Protocol:      api.ProtocolTCP,
							TargetPort:    80,
							PublishedPort: endpoint.Ports[0].PublishedPort,
						},
					},
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(q.IsNoop()).To(BeFalse())
			})
			It("should not free any other ports", func() {
				p.Commit()

				_, err := pa.Allocate(&api.Endpoint{}, &api.EndpointSpec{
					Ports: p.Ports()[1:],
				})
				Expect(err).To(HaveOccurred())
				Expect(errors.IsErrResourceInUse(err)).To(BeTrue())
			})
		})
		Context("to change protocol", func() {
			var (
				endpointCopy *api.Endpoint
				p            Proposal
				err          error
			)
			BeforeEach(func() {
				endpointCopy = endpoint.Copy()
				spec := endpoint.Spec.Copy()
				spec.Ports[0].Protocol = api.ProtocolSCTP
				p, err = pa.Allocate(endpointCopy, spec)
			})
			It("should allocate the new port", func() {
				Expect(err).ToNot(HaveOccurred())
				Expect(p.IsNoop()).ToNot(BeTrue())

				e := endpoint.Copy()
				e.Ports[0].Protocol = api.ProtocolSCTP
				Expect(p.Ports()).To(ConsistOf(e.Ports))

				p.Commit()
				_, err := pa.Allocate(&api.Endpoint{}, &api.EndpointSpec{
					Ports: p.Ports()[0:1],
				})
				Expect(err).To(HaveOccurred())
				Expect(errors.IsErrResourceInUse(err)).To(BeTrue())
			})
			It("should free the old port", func() {
				p.Commit()
				_, err := pa.Allocate(&api.Endpoint{}, &api.EndpointSpec{
					Ports: endpoint.Ports[0:1],
				})
				Expect(err).ToNot(HaveOccurred())
			})
		})
		Context("to change publish mode", func() {
			Context("from ingress to host", func() {
				var (
					p   Proposal
					err error
				)
				BeforeEach(func() {
					spec := endpoint.Spec.Copy()
					// port 80
					spec.Ports[0].PublishMode = api.PublishModeHost
					p, err = pa.Allocate(endpoint, spec)
				})
				It("should succeed", func() {
					Expect(err).ToNot(HaveOccurred())
					Expect(p).ToNot(BeNil())
				})
				It("should deallocate the port changed to host mode", func() {
					Expect(p.IsNoop()).To(BeFalse())
					p.Commit()
					// try allocating port 80
					spec2 := &api.EndpointSpec{
						Ports: []*api.PortConfig{
							{
								Protocol:      api.ProtocolTCP,
								TargetPort:    80,
								PublishedPort: 80,
								PublishMode:   api.PublishModeIngress,
							},
						},
					}
					q, e := pa.Allocate(&api.Endpoint{}, spec2)
					Expect(e).ToNot(HaveOccurred())
					Expect(q).ToNot(BeNil())
					Expect(q.IsNoop()).To(BeFalse())
				})
			})
			Context("from host to ingress", func() {
				var (
					endpoint2 *api.Endpoint
					p         Proposal
					err       error
				)
				BeforeEach(func() {
					spec2 := &api.EndpointSpec{
						Ports: []*api.PortConfig{
							{
								Protocol:      api.ProtocolTCP,
								PublishedPort: 911,
								TargetPort:    80,
								PublishMode:   api.PublishModeHost,
							}, {
								Protocol:      api.ProtocolTCP,
								PublishedPort: 912,
								TargetPort:    81,
								PublishMode:   api.PublishModeHost,
							},
						},
					}
					endpoint2 = &api.Endpoint{}
					// this is setup mirroring a well-tested scenario, so just
					// ignore the error and segfault if q is nil
					q, _ := pa.Allocate(endpoint2, spec2)
					endpoint2.Spec = spec2.Copy()
					endpoint2.Ports = q.Ports()
					q.Commit()

					spec2.Ports[0].PublishMode = api.PublishModeIngress
					p, err = pa.Allocate(endpoint2, spec2)
				})
				It("should succeed", func() {
					Expect(err).ToNot(HaveOccurred())
					Expect(p).ToNot(BeNil())
				})
				It("should reserve the changed port", func() {
					p.Commit()
					spec := &api.EndpointSpec{
						Ports: []*api.PortConfig{
							{
								Protocol:      api.ProtocolTCP,
								PublishedPort: 911,
								TargetPort:    80,
								PublishMode:   api.PublishModeIngress,
							},
						},
					}
					_, e := pa.Allocate(&api.Endpoint{}, spec)
					Expect(e).To(HaveOccurred())
					Expect(e).To(WithTransform(errors.IsErrResourceInUse, BeTrue()))
					Expect(e.Error()).To(Equal("port 911/TCP is in use"))
				})
			})
		})
	})

	Describe("dyanmic port allocation", func() {
		Context("adding a port with no PublishPort specified", func() {
			var (
				spec     *api.EndpointSpec
				specCopy *api.EndpointSpec
				p        Proposal
				err      error
			)
			BeforeEach(func() {
				spec = &api.EndpointSpec{
					Ports: []*api.PortConfig{
						{
							Name:        "http",
							Protocol:    api.ProtocolTCP,
							TargetPort:  80,
							PublishMode: api.PublishModeIngress,
						},
					},
				}
				specCopy = spec.Copy()
				p, err = pa.Allocate(&api.Endpoint{}, spec)
			})

			It("should succeed", func() {
				Expect(err).ToNot(HaveOccurred())
				Expect(p.IsNoop()).To(BeFalse())
			})

			It("should assign a port from the dynamic port range", func() {
				Expect(p.Ports()).ToNot(BeEmpty())
				Expect(PortsMostlyEqual(p.Ports()[0], spec.Ports[0])).To(BeTrue())

				Expect(p.Ports()[0].PublishedPort).To(And(
					BeNumerically(">=", DynamicPortStart),
					BeNumerically("<=", DynamicPortEnd),
				))
			})

			It("should be idempotent", func() {
				endpoint := &api.Endpoint{
					Spec:  spec,
					Ports: p.Ports(),
				}
				p.Commit()
				for i := 0; i < 5; i++ {
					q, err := pa.Allocate(endpoint, spec)
					Expect(err).ToNot(HaveOccurred())
					Expect(q.IsNoop()).To(BeTrue())
					Expect(q.Ports()).To(ConsistOf(endpoint.Ports))
					// This should be covered by the above case but I'm being
					// extra cautious
					Expect(q.Ports()[0].PublishedPort).To(Equal(endpoint.Ports[0].PublishedPort))
					q.Commit()
				}
			})

			It("should not modify the spec", func() {
				Expect(spec).To(Equal(specCopy))
			})

			It("should cause new allocations to take the next available port", func() {
				p.Commit()

				s := spec.Copy()
				q, err := pa.Allocate(&api.Endpoint{}, s)

				Expect(err).NotTo(HaveOccurred())
				Expect(q.IsNoop()).To(BeFalse())
				Expect(q.Ports()).ToNot(BeEmpty())
				Expect(q.Ports()[0].PublishedPort).To(And(
					BeNumerically(">=", DynamicPortStart),
					BeNumerically("<=", DynamicPortEnd),
					Not(Equal(p.Ports()[0].PublishedPort)),
				))
			})
		})

		Context("when the dynamic port space is exhausted", func() {
			var (
				endpoint *api.Endpoint
				e        error
			)
			BeforeEach(func() {
				// variables to use to capture the last created endpoint
				var (
					spec *api.EndpointSpec
					prop Proposal
					err  error
				)
				for i := DynamicPortStart; i <= DynamicPortEnd; i++ {
					spec = &api.EndpointSpec{
						Ports: []*api.PortConfig{
							{
								Name:        fmt.Sprintf("dynamic-%v", i),
								Protocol:    api.ProtocolTCP,
								TargetPort:  80,
								PublishMode: api.PublishModeIngress,
							},
						},
					}

					prop, err = pa.Allocate(&api.Endpoint{}, spec)
					if err != nil {
						e = err
						break
					}
					prop.Commit()
				}
				endpoint = &api.Endpoint{
					Spec:  spec.Copy(),
					Ports: prop.Ports(),
				}
			})

			It("should fill all the way up successfully", func() {
				Expect(e).ToNot(HaveOccurred())
			})

			It("should not prevent dynamic allocation in a different port space", func() {
				spec := &api.EndpointSpec{
					Ports: []*api.PortConfig{
						{
							Name:        "shouldsucceed",
							Protocol:    api.ProtocolSCTP,
							TargetPort:  80,
							PublishMode: api.PublishModeIngress,
						},
					},
				}

				prop, err := pa.Allocate(&api.Endpoint{}, spec)
				Expect(err).ToNot(HaveOccurred())
				Expect(prop).ToNot(BeNil())
				Expect(prop.IsNoop()).To(BeFalse())
			})
			Context("adding a port to the same space", func() {
				var (
					endpoint *api.Endpoint
					spec     *api.EndpointSpec
				)
				BeforeEach(func() {
					endpoint = &api.Endpoint{}
					spec = &api.EndpointSpec{
						Ports: []*api.PortConfig{
							{
								Name:        "shouldfail",
								Protocol:    api.ProtocolTCP,
								TargetPort:  80,
								PublishMode: api.PublishModeIngress,
							},
						},
					}
				})
				It("should return ErrResourceExhausted", func() {
					_, err := pa.Allocate(endpoint, spec)
					Expect(err).To(HaveOccurred())
					Expect(err).To(WithTransform(
						errors.IsErrResourceExhausted,
						BeTrue(),
					))
					Expect(err.Error()).To(Equal(
						"resource dynamic port space is exhausted: protocol TCP",
					))
				})
			})
			Context("when removing one port and adding another", func() {
				var (
					p   Proposal
					err error
				)
				BeforeEach(func() {
					spec := endpoint.Spec.Copy()
					spec.Ports = []*api.PortConfig{
						{
							Name:       "newport",
							Protocol:   api.ProtocolTCP,
							TargetPort: 80,
							// No PublishedPort, so we should try dynamically
							// allocating
							PublishMode: api.PublishModeIngress,
						},
					}
					p, err = pa.Allocate(endpoint, spec)
				})
				It("should succeed", func() {
					Expect(err).ToNot(HaveOccurred())
					Expect(p).ToNot(BeNil())
					// ports changed, but net allocation didn't, so this will
					// be a noop
					Expect(p.IsNoop()).To(BeTrue())
				})
			})
			Context("when adding a new, static published port", func() {
				var (
					p   Proposal
					err error
				)
				BeforeEach(func() {
					spec := endpoint.Spec.Copy()
					spec.Ports = append(spec.Ports, &api.PortConfig{
						Protocol:      api.ProtocolTCP,
						TargetPort:    8080,
						PublishedPort: 8080,
						PublishMode:   api.PublishModeIngress,
					})
					p, err = pa.Allocate(endpoint, spec)
				})
				It("should succeed", func() {
					Expect(err).ToNot(HaveOccurred())
					Expect(p).ToNot(BeNil())
					Expect(p.IsNoop()).To(BeFalse())
				})
			})
		})

		Context("when a port in the dynamic range desired to be statically allocated", func() {
			var (
				spec *api.EndpointSpec
				p    Proposal
				err  error
			)
			BeforeEach(func() {
				// NOTE(dperny): this test relies on the fact that dynamic port
				// allocation is sequential. If the behavior of dynamic port
				// allocation changes, this test will need to be altered.
				spec = &api.EndpointSpec{
					Ports: []*api.PortConfig{
						{
							Protocol:    api.ProtocolTCP,
							TargetPort:  80,
							PublishMode: api.PublishModeIngress,
						},
						{
							Protocol:      api.ProtocolTCP,
							TargetPort:    80,
							PublishedPort: DynamicPortStart,
							PublishMode:   api.PublishModeIngress,
						},
					},
				}
				p, err = pa.Allocate(&api.Endpoint{}, spec)
			})
			It("should succeed", func() {
				Expect(err).ToNot(HaveOccurred())
				Expect(p).ToNot(BeNil())
				Expect(p.IsNoop()).ToNot(BeTrue())
				Expect(p.Ports()).ToNot(BeEmpty())
			})
			It("should give the statically allocated port first priority", func() {
				Expect(p.Ports()[1].PublishedPort).To(Equal(DynamicPortStart))
				Expect(p.Ports()[0].PublishedPort).To(And(
					BeNumerically(">=", DynamicPortStart),
					BeNumerically("<=", DynamicPortEnd),
					Not(Equal(DynamicPortStart)),
				))
			})
		})
		Context("when changing ports", func() {
			var (
				endpoint *api.Endpoint
				spec     *api.EndpointSpec
			)
			BeforeEach(func() {
				spec = &api.EndpointSpec{
					Ports: []*api.PortConfig{
						{
							Protocol:    api.ProtocolTCP,
							TargetPort:  80,
							PublishMode: api.PublishModeIngress,
						},
						{
							Protocol:      api.ProtocolTCP,
							TargetPort:    80,
							PublishedPort: 80,
							PublishMode:   api.PublishModeIngress,
						},
					},
				}
				endpoint = &api.Endpoint{
					Spec:  spec,
					Ports: spec.Ports,
				}
				endpoint.Ports[0].PublishedPort = DynamicPortStart
				pa.Restore([]*api.Endpoint{endpoint})
			})
			Context("from dynamic to statically allocated", func() {
				var (
					specCopy *api.EndpointSpec
					p        Proposal
					e        error
				)
				BeforeEach(func() {
					specCopy = spec.Copy()
					specCopy.Ports[0].PublishedPort = 8080

					p, e = pa.Allocate(endpoint, specCopy)
				})
				It("should succeed", func() {
					Expect(e).ToNot(HaveOccurred())
					Expect(p).ToNot(BeNil())
					Expect(p.IsNoop()).ToNot(BeTrue())
					Expect(p.Ports()).ToNot(BeEmpty())
				})
				It("should free the previously allocated dynamic port", func() {
					p.Commit()
					spec2 := &api.EndpointSpec{
						Ports: []*api.PortConfig{
							{
								Protocol:      api.ProtocolTCP,
								TargetPort:    80,
								PublishedPort: endpoint.Ports[0].PublishedPort,
								PublishMode:   api.PublishModeIngress,
							},
						},
					}
					_, err := pa.Allocate(&api.Endpoint{}, spec2)
					Expect(err).ToNot(HaveOccurred())
				})
			})
			Context("from statically to dynamically allocated", func() {
				var (
					specCopy *api.EndpointSpec
					p        Proposal
					e        error
				)
				BeforeEach(func() {
					specCopy = spec.Copy()
					specCopy.Ports[1].PublishedPort = 0

					p, e = pa.Allocate(endpoint, specCopy)
				})
				It("should succeed", func() {
					Expect(e).ToNot(HaveOccurred())
					Expect(p).ToNot(BeNil())
					Expect(p.IsNoop()).To(BeFalse())
					Expect(p.Ports()).NotTo(BeEmpty())
				})
				It("should free the old static allocation", func() {
					p.Commit()

					s := &api.EndpointSpec{
						Ports: []*api.PortConfig{
							{
								Protocol:      api.ProtocolTCP,
								TargetPort:    80,
								PublishedPort: 80,
								PublishMode:   api.PublishModeIngress,
							},
						},
					}

					_, err := pa.Allocate(&api.Endpoint{}, s)
					Expect(err).ToNot(HaveOccurred())
				})
				It("should dynamically allocate a new port number", func() {
					Expect(p.Ports()[1].PublishedPort).To(And(
						BeNumerically(">=", DynamicPortStart),
						BeNumerically("<=", DynamicPortEnd),
						Not(Equal(spec.Ports[1].PublishedPort)),
					))
				})
			})
		})
	})

	Describe("endpoints with publish mode host ports", func() {
		Context("when adding a host port", func() {
			var (
				spec *api.EndpointSpec
				p    Proposal
				e    error
			)
			BeforeEach(func() {
				spec = &api.EndpointSpec{
					Ports: []*api.PortConfig{
						{
							TargetPort:    80,
							PublishedPort: 80,
							PublishMode:   api.PublishModeHost,
						},
					},
				}
				p, e = pa.Allocate(&api.Endpoint{}, spec)
			})
			It("should succeed", func() {
				Expect(e).ToNot(HaveOccurred())
				Expect(p).ToNot(BeNil())
			})
			It("should be a noop", func() {
				Expect(p.IsNoop()).To(BeTrue())
			})
			It("should include the host ports in the returned ports", func() {
				Expect(p.Ports()).To(ConsistOf(spec.Ports))
			})
			It("should not mark any ports in use", func() {
				s := spec.Copy()
				s.Ports[0].PublishMode = api.PublishModeIngress
				_, err := pa.Allocate(&api.Endpoint{}, s)
				Expect(err).ToNot(HaveOccurred())
			})
			It("should be idemopotent", func() {
				p.Commit()

				s := spec.Copy()
				endpoint := &api.Endpoint{
					Spec:  s,
					Ports: p.Ports(),
				}

				for i := 0; i < 5; i++ {
					q, err := pa.Allocate(endpoint, s)
					Expect(err).ToNot(HaveOccurred())
					Expect(q.IsNoop()).To(BeTrue())
					Expect(q.Ports()).To(ConsistOf(endpoint.Ports))
					q.Commit()
				}
			})
			Context("when adding another host port with no PublishPort specified", func() {
				var (
					specCopy *api.EndpointSpec
					endpoint *api.Endpoint
					q        Proposal
					e        error
				)
				BeforeEach(func() {
					specCopy = spec.Copy()
					endpoint = &api.Endpoint{}
					endpoint.Ports = p.Ports()
					endpoint.Spec = spec
					specCopy.Ports = append(spec.Ports, &api.PortConfig{
						Name:        "another",
						PublishMode: api.PublishModeHost,
						TargetPort:  80,
						Protocol:    api.ProtocolTCP,
					})
					q, e = pa.Allocate(endpoint, specCopy)
				})
				It("should succeed", func() {
					Expect(e).ToNot(HaveOccurred())
					Expect(q).ToNot(BeNil())
				})
				It("should be a noop", func() {
					Expect(q.IsNoop()).To(BeTrue())
				})
				It("should not fill in the published port", func() {
					Expect(q.Ports()).To(ConsistOf(specCopy.Ports))
				})
			})
		})
	})

	Describe("deallocate", func() {
		var (
			endpoint  *api.Endpoint
			endpoint2 *api.Endpoint
			p         Proposal
		)

		BeforeEach(func() {
			spec := &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{
						Name:          "http",
						Protocol:      api.ProtocolUDP,
						TargetPort:    80,
						PublishedPort: 80,
						PublishMode:   api.PublishModeIngress,
					},
					{
						Name:          "https",
						Protocol:      api.ProtocolTCP,
						TargetPort:    443,
						PublishedPort: 443,
						PublishMode:   api.PublishModeIngress,
					},
					{
						Name:          "host",
						Protocol:      api.ProtocolTCP,
						TargetPort:    80,
						PublishedPort: 8080,
						PublishMode:   api.PublishModeHost,
					},
				},
			}
			endpoint = &api.Endpoint{
				Spec:  spec,
				Ports: spec.Ports,
			}
			endpoint2 = endpoint.Copy()
			endpoint2.Ports[0].Protocol = api.ProtocolTCP
			endpoint2.Ports[1].Protocol = api.ProtocolUDP
			endpoint2.Ports[2].PublishMode = api.PublishModeIngress
			endpoint2.Spec.Ports = endpoint2.Ports
			pa.Restore([]*api.Endpoint{endpoint, endpoint2})

			p = pa.Deallocate(endpoint)
			p.Commit()
		})
		It("should return a proposal that isn't a noop but has no ports", func() {
			Expect(p.IsNoop()).To(BeFalse())
			Expect(p.Ports()).To(BeEmpty())
		})
		It("should not modify the spec", func() {
			Expect(endpoint.Spec).ToNot(BeNil())
			Expect(endpoint.Ports).To(ConsistOf(endpoint.Spec.Ports))
			Expect(endpoint.Spec.Ports).To(HaveLen(3))
		})
		It("should free any ports in the endpoint", func() {
			q, err := pa.Allocate(&api.Endpoint{}, endpoint.Spec)
			Expect(err).ToNot(HaveOccurred())
			Expect(q).ToNot(BeNil())
			Expect(q.Ports()).To(ConsistOf(endpoint.Spec.Ports))
			Expect(q.IsNoop()).To(BeFalse())
		})
		It("should only free endpoints of the correct protocol", func() {
			q, err := pa.Allocate(&api.Endpoint{}, endpoint2.Spec)
			Expect(q).To(BeNil())
			Expect(err).To(HaveOccurred())
			Expect(err).To(WithTransform(errors.IsErrResourceInUse, BeTrue()))
		})
		It("should not free any ports with publish mode host", func() {
			spec := endpoint2.Spec.Copy()
			// take only the last, ingress-mode port
			spec.Ports = spec.Ports[2:]
			q, err := pa.Allocate(&api.Endpoint{}, spec)
			Expect(q).To(BeNil())
			Expect(err).To(HaveOccurred())
			Expect(err).To(WithTransform(errors.IsErrResourceInUse, BeTrue()))
		})
	})

})

var _ = Describe("AlreadyAllocated", func() {
	Context("endpoint and spec are nil", func() {
		It("should return true", func() {
			Expect(AlreadyAllocated(nil, nil)).To(BeTrue())
		})
	})
	Context("endpoint is nil and spec is not", func() {
		It("should return false", func() {
			Expect(AlreadyAllocated(nil, &api.EndpointSpec{})).To(BeFalse())
		})
	})
	Context("the endpoint's spec is nil, but the spec is not", func() {
		It("should return false", func() {
			Expect(AlreadyAllocated(&api.Endpoint{}, &api.EndpointSpec{})).To(BeFalse())
		})
	})
	Context("spec is nil, and endpoint.Spec is not nil and has more than 0 ports", func() {
		endpoint := &api.Endpoint{
			Spec: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{},
				},
			},
		}
		It("should return false", func() {
			Expect(AlreadyAllocated(endpoint, nil)).To(BeFalse())
		})
	})
	Context("spec and endpoint have different numbers of ports", func() {
		endpoint := &api.Endpoint{
			Spec: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{},
				},
			},
		}
		spec := &api.EndpointSpec{
			Ports: []*api.PortConfig{
				{}, {},
			},
		}
		It("should return false", func() {
			Expect(AlreadyAllocated(endpoint, spec)).To(BeFalse())
		})
	})
	Context("spec and endpoint ports are the same", func() {
		endpoint := &api.Endpoint{
			Spec: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{},
				},
			},
		}
		spec := &api.EndpointSpec{
			Ports: []*api.PortConfig{
				{},
			},
		}

		It("should return true", func() {
			Expect(AlreadyAllocated(endpoint, spec)).To(BeTrue())
		})
	})
	Context("spec and endpoint ports are different", func() {
		endpoint := &api.Endpoint{
			Spec: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{},
				},
			},
		}
		spec := &api.EndpointSpec{
			Ports: []*api.PortConfig{
				{PublishedPort: 80},
			},
		}
		It("should return false", func() {
			Expect(AlreadyAllocated(endpoint, spec)).To(BeFalse())
		})
	})
})
