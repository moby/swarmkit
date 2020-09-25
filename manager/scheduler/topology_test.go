package scheduler

import (
	"github.com/docker/swarmkit/api"
	"testing"
)

func TestIsInTopology(t *testing.T) {
	for _, tc := range []struct {
		top        *api.Topology
		accessible []*api.Topology
		expected   bool
	}{
		{
			// simple match of top to accessible.
			top: &api.Topology{
				Segments: map[string]string{
					"region": "R1",
					"zone":   "Z1",
				},
			},
			accessible: []*api.Topology{
				{
					Segments: map[string]string{
						"region": "R1",
						"zone":   "Z1",
					},
				},
			},
			expected: true,
		}, {
			// top is explicitly one of accessible.
			top: &api.Topology{
				Segments: map[string]string{
					"region": "R1",
					"zone":   "Z2",
				},
			},
			accessible: []*api.Topology{
				{
					Segments: map[string]string{
						"region": "R1",
						"zone":   "Z1",
					},
				}, {
					Segments: map[string]string{
						"region": "R1",
						"zone":   "Z2",
					},
				},
			},
			expected: true,
		}, {
			// top is implicitly one of accessible, by virtue of no conflicts.
			// only the region is specified in the accessible topology.
			//
			// NOTE(dperny): I am unsure if this is, by the CSI standard, a
			// supported case
			top: &api.Topology{
				Segments: map[string]string{
					"region": "R1",
					"zone":   "Z3",
				},
			},
			accessible: []*api.Topology{
				{
					Segments: map[string]string{
						"region": "R1",
					},
				},
			},
			expected: true,
		}, {
			// top is explicitly not in accessible topology
			top: &api.Topology{
				Segments: map[string]string{
					"region": "R1",
					"zone":   "Z1",
				},
			},
			accessible: []*api.Topology{
				{
					Segments: map[string]string{
						"region": "R2",
						"zone":   "Z1",
					},
				},
			},
			expected: false,
		},
		{
			// accessible specifies two subdomains, top contains 3. top is in
			// accessible.
			top: &api.Topology{
				Segments: map[string]string{
					"region": "R1",
					"zone":   "Z1",
					"shelf":  "S1",
				},
			},
			accessible: []*api.Topology{
				{
					Segments: map[string]string{
						"region": "R1",
						"zone":   "Z1",
					},
				}, {
					Segments: map[string]string{
						"region": "R1",
						"zone":   "Z2",
					},
				},
			},
			expected: true,
		}, {
			top: &api.Topology{
				Segments: map[string]string{
					"region": "R1",
					"zone":   "Z1",
					"shelf":  "S1",
				},
			},
			accessible: []*api.Topology{
				{
					Segments: map[string]string{
						"region": "R1",
						"zone":   "Z1",
						"shelf":  "S2",
					},
				}, {
					Segments: map[string]string{
						"region": "R1",
						"zone":   "Z2",
						"shelf":  "S1",
					},
				},
			},
			expected: false,
		}, {
			top: &api.Topology{
				Segments: map[string]string{
					"region": "R1",
					"zone":   "Z1",
					"shelf":  "S1",
				},
			},
			accessible: []*api.Topology{
				{
					Segments: map[string]string{
						"region": "R1",
						"zone":   "Z1",
						"shelf":  "S2",
					},
				}, {
					Segments: map[string]string{
						"region": "R1",
						"zone":   "Z2",
						"shelf":  "S1",
					},
				}, {
					Segments: map[string]string{
						"region": "R1",
						"zone":   "Z1",
						"shelf":  "S1",
					},
				},
			},
			expected: true,
		},
	} {
		actual := IsInTopology(tc.top, tc.accessible)
		if actual != tc.expected {
			t.Errorf("Expected %v to lie within %v", tc.top, tc.accessible)
		}
	}
}
