package namespaces

import (
	"fmt"
	"reflect"
	"testing"
)

func TestContextExpansion(t *testing.T) {
	for _, testcase := range []struct {
		ctx      *Context
		inputs   []Name
		expected [][]Name
	}{
		{
			ctx:    &DefaultContext,
			inputs: []Name{"redis", "postgres", "frontend"},
			expected: [][]Name{
				[]Name{
					"redis.default",
				},
				[]Name{
					"postgres.default",
				},
				[]Name{
					"frontend.default",
				},
			},
		},
		{
			ctx: &Context{
				Namespace: "myapp",
				Search: []Name{
					"default",
					"development",
				},
			},
			inputs: []Name{"redis", "postgres", "frontend"},
			expected: [][]Name{
				[]Name{
					"redis.myapp",
					"redis.default",
					"redis.development",
				},
				[]Name{
					"postgres.myapp",
					"postgres.default",
					"postgres.development",
				},
				[]Name{
					"frontend.myapp",
					"frontend.default",
					"frontend.development",
				},
			},
		},
	} {

		fatalf := func(format string, args ...interface{}) {
			args = append([]interface{}{testcase.ctx, testcase.inputs}, args...)
			t.Fatalf("context=%v, inputs=%v: "+format, args...)
		}
		for i, input := range testcase.inputs {
			candidates, err := testcase.ctx.Candidates(input)
			if err != nil {
				fatalf("unexpected error getting candidates: %v", err)
			}

			if !reflect.DeepEqual(candidates, testcase.expected[i]) {
				fatalf("%v != %v", candidates, testcase.expected[i])
			}

			// test local resolution
			resolved, err := testcase.ctx.Resolve(input, ResolutionOptions{})
			if err != nil {
				fatalf("unexpected error resolving local reference: %v", err)
			}

			if resolved != input {
				fatalf("local reference not resolved: %v != %v", resolved, input)
			}

			// Make every resolution qualified
			resolved, err = testcase.ctx.Resolve(input, ResolutionOptions{Qualify: true})
			if err != nil {
				fatalf("unexpected error resolving local reference: %v", err)
			}

			expectedQualified, err := input.Join(testcase.ctx.Namespace)
			if err != nil {
				fatalf("unexpected error calculating expected qualified name: %v", err)
			}

			if resolved != expectedQualified {
				fatalf("unexpected qualified reference: ", resolved, expectedQualified)
			}

			// filter local references, simulating missing resources in namespace.
			resolved, err = testcase.ctx.Resolve(input, ResolutionOptions{
				Qualify: true,
				Match: func(name Name) bool {
					return !(name == input || name == expectedQualified)
				},
			})

			// if the search space is empty, we expect a resolution error
			if len(testcase.ctx.Search) == 0 {
				if err != ErrNameUnresolved {
					fatalf("unexpected error resolving external name in namespace without searchspace: %v != %v", err, ErrNameUnresolved)
				}
			} else {
				if err != nil {
					fatalf("unexpected error filtering resolution: %v", err)
				}

				// the first match should be the first expected
				if resolved != testcase.expected[i][1] {
					fatalf("resolved external reference incorrect: %v != %v", resolved, testcase.expected[i][1])
				}
			}
		}
	}
}

// TestContextApplication demonstrates a few use cases of context.
func TestContextApplication(t *testing.T) {
	ns1 := namespace{
		name:   "ns1",
		search: []Name{"ns2", "ns3"},
		resources: []resource{
			{
				name: "www",
				references: []Name{
					"frontend",
					"backend",
				},
			},
			{
				name: "redis",
				references: []Name{
					"backend",
					"redis-data",
				},
			},
		},
	}

	// run the same app, but declare volume redis-data
	ns2 := namespace{
		name:   "ns2",
		search: []Name{"ns3"},
		resources: []resource{
			{
				name: "www",
				references: []Name{
					"frontend",
					"backend",
				},
			},
			{
				name: "redis",
				references: []Name{
					"backend",
					"redis-data",
				},
			},
			{
				name: "redis-data",
			},
		},
	}

	// define the networks separately
	ns3 := namespace{
		name: "ns3",
		resources: []resource{
			{
				name: "frontend",
			},
			{
				name: "backend",
			},
		},
	}

	cluster := cluster{
		namespaces: []namespace{ns1, ns2, ns3},
	}

	// loop through the namespaces, resolving references against the cluster.
	for _, ns := range []namespace{ns1, ns2, ns3} {
		fmt.Println("namespace", ns.name)
		var indent string
		printlni := func(args ...interface{}) {
			fmt.Println(append([]interface{}{indent}, args...)...)
		}

		indent = "\t"
		for _, r := range ns.resources {
			printlni(r.name, "==", r.qualify(ns.context()))

			indent = "\t\t"

			for _, r := range r.references {
				name, err := cluster.resolve(ns.context(), r)
				if err != nil {
					if err != ErrNameUnresolved {
						panic(err)
					}

					printlni(r)
				} else {
					printlni(r, "==", name)
				}
			}

			indent = "\t"

		}
		indent = ""
	}

	t.Fail()
}

// resource is a named resource within a namespace that references other
// resources, which may or may not be a part of the current namespace.
type resource struct {
	name       Name
	references []Name
}

func (r *resource) qualify(ctx *Context) Name {
	q, err := r.name.Join(ctx.Namespace)
	if err != nil {
		panic(err)
	}

	return q
}

// namespace simulates an application namespace and all associated
// resources.
type namespace struct {
	name      Name
	resources []resource
	search    []Name
}

func (ns namespace) context() *Context {
	return &Context{Namespace: ns.name, Search: ns.search}
}

// defined returns a set of names that are defined in the namespace.
func (ns namespace) defined() []Name {
	var names []Name
	for _, resource := range ns.resources {
		names = append(names, resource.name)
	}

	return names
}

func (ns namespace) qualified() []Name {
	names := ns.defined()
	for i := range names {
		var err error
		names[i], err = names[i].Join(ns.name)
		if err != nil {
			panic(err)
		}
	}

	return names
}

func (ns namespace) references() []Name {
	var references []Name
	for _, resource := range ns.resources {
		references = append(references, resource.references...)
	}
	return references
}

// cluster simlulates the cluster
type cluster struct {
	namespaces []namespace
}

// exists returns true if the qualified name exists in the cluster.
func (c *cluster) exists(name Name) bool {
	for _, ns := range c.namespaces {
		for _, q := range ns.qualified() {
			if q == name {
				return true
			}
		}
	}

	return false
}

func (c *cluster) resolve(ctx *Context, name Name) (Name, error) {
	return ctx.Resolve(name, ResolutionOptions{Qualify: true, Match: c.exists})
}
