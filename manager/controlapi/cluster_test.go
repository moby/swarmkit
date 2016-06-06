package controlapi

import (
	"testing"
	"time"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/ca/testutils"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/docker/swarm-v2/protobuf/ptypes"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func createClusterSpec(name string) *api.ClusterSpec {
	return &api.ClusterSpec{
		Annotations: api.Annotations{
			Name: name,
		},
		CAConfig: api.CAConfig{
			NodeCertExpiry: ptypes.DurationProto(ca.DefaultNodeCertExpiration),
		},
	}
}

func createCluster(t *testing.T, ts *testServer, id, name string, policy api.AcceptancePolicy) *api.Cluster {
	spec := createClusterSpec(name)
	spec.AcceptancePolicy = policy

	cluster := &api.Cluster{
		ID:   id,
		Spec: *spec,
	}
	assert.NoError(t, ts.Store.Update(func(tx store.Tx) error {
		return store.CreateCluster(tx, cluster)
	}))
	return cluster
}

func TestValidateClusterSpec(t *testing.T) {
	type BadClusterSpec struct {
		spec *api.ClusterSpec
		c    codes.Code
	}

	for _, bad := range []BadClusterSpec{
		{
			spec: nil,
			c:    codes.InvalidArgument,
		},
		{
			spec: &api.ClusterSpec{
				Annotations: api.Annotations{
					Name: "name",
				},
				CAConfig: api.CAConfig{
					NodeCertExpiry: ptypes.DurationProto(29 * time.Minute),
				},
			},
			c: codes.InvalidArgument,
		},
	} {
		err := validateClusterSpec(bad.spec)
		assert.Error(t, err)
		assert.Equal(t, bad.c, grpc.Code(err))
	}

	for _, good := range []*api.ClusterSpec{
		createClusterSpec("name"),
	} {
		err := validateClusterSpec(good)
		assert.NoError(t, err)
	}
}

func TestGetCluster(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{ClusterID: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	cluster := createCluster(t, ts, "name", "name", testutils.AutoAcceptPolicy())
	r, err := ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{ClusterID: cluster.ID})
	assert.NoError(t, err)
	cluster.Meta.Version = r.Cluster.Meta.Version
	assert.Equal(t, cluster, r.Cluster)
}

func TestGetClusterWithSecret(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{ClusterID: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	policy := testutils.AutoAcceptPolicy()
	policy.Secret = "secret"
	cluster := createCluster(t, ts, "name", "name", policy)
	r, err := ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{ClusterID: cluster.ID})
	assert.NoError(t, err)
	cluster.Meta.Version = r.Cluster.Meta.Version
	assert.NotEqual(t, cluster, r.Cluster)
	assert.Contains(t, r.Cluster.String(), "[REDACTED]")
	assert.Contains(t, cluster.String(), "secret")
}

func TestGetClusterRedactsPrivateKeys(t *testing.T) {
	ts := newTestServer(t)

	cluster := createCluster(t, ts, "name", "name", testutils.AutoAcceptPolicy())
	r, err := ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{ClusterID: cluster.ID})
	assert.NoError(t, err)
	assert.NotContains(t, r.Cluster.String(), "PRIVATE")
}

func TestUpdateCluster(t *testing.T) {
	ts := newTestServer(t)
	cluster := createCluster(t, ts, "name", "name", api.AcceptancePolicy{})

	_, err := ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{ClusterID: "invalid", Spec: &cluster.Spec, ClusterVersion: &api.Version{}})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	// No update options.
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{ClusterID: cluster.ID, Spec: &cluster.Spec})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{ClusterID: cluster.ID, Spec: &cluster.Spec, ClusterVersion: &cluster.Meta.Version})
	assert.NoError(t, err)

	r, err := ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{
		Filters: &api.ListClustersRequest_Filters{
			Names: []string{"name"},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, r.Clusters, 1)
	assert.Equal(t, cluster.Spec.Annotations.Name, r.Clusters[0].Spec.Annotations.Name)
	assert.Len(t, r.Clusters[0].Spec.AcceptancePolicy.Autoaccept, 0)

	r.Clusters[0].Spec.AcceptancePolicy.Autoaccept = map[string]bool{"manager": true}
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterID:      cluster.ID,
		Spec:           &r.Clusters[0].Spec,
		ClusterVersion: &r.Clusters[0].Meta.Version,
	})
	assert.NoError(t, err)

	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{
		Filters: &api.ListClustersRequest_Filters{
			Names: []string{"name"},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, r.Clusters, 1)
	assert.Equal(t, cluster.Spec.Annotations.Name, r.Clusters[0].Spec.Annotations.Name)
	assert.Len(t, r.Clusters[0].Spec.AcceptancePolicy.Autoaccept, 1)

	r.Clusters[0].Spec.AcceptancePolicy.Secret = "secret"
	returnedCluster, err := ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterID:      cluster.ID,
		Spec:           &r.Clusters[0].Spec,
		ClusterVersion: &r.Clusters[0].Meta.Version,
	})
	assert.NoError(t, err)
	assert.NotContains(t, returnedCluster.String(), "secret")
	assert.Contains(t, returnedCluster.String(), "[REDACTED]")

	// Versioning.
	assert.NoError(t, err)
	version := &returnedCluster.Cluster.Meta.Version

	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterID:      cluster.ID,
		Spec:           &r.Clusters[0].Spec,
		ClusterVersion: version,
	})
	assert.NoError(t, err)

	// Perform an update with the "old" version.
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterID:      cluster.ID,
		Spec:           &r.Clusters[0].Spec,
		ClusterVersion: version,
	})
	assert.Error(t, err)
}

func TestListClusters(t *testing.T) {
	ts := newTestServer(t)
	r, err := ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Empty(t, r.Clusters)

	createCluster(t, ts, "id1", "name1", testutils.AutoAcceptPolicy())
	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Clusters))

	createCluster(t, ts, "id2", "name2", testutils.AutoAcceptPolicy())
	createCluster(t, ts, "id3", "name3", testutils.AutoAcceptPolicy())
	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Clusters))
}

func TestListClustersWithSecrets(t *testing.T) {
	ts := newTestServer(t)
	r, err := ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Empty(t, r.Clusters)

	policy := testutils.AutoAcceptPolicy()
	policy.Secret = "secret"

	createCluster(t, ts, "id1", "name1", policy)
	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Clusters))

	createCluster(t, ts, "id2", "name2", policy)
	createCluster(t, ts, "id3", "name3", policy)
	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Clusters))
	for _, cluster := range r.Clusters {
		assert.NotContains(t, cluster.String(), policy.Secret)
		assert.Contains(t, cluster.String(), "[REDACTED]")
	}
}

func TestListClustersRedactsPrivateKey(t *testing.T) {
	ts := newTestServer(t)
	r, err := ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Empty(t, r.Clusters)

	createCluster(t, ts, "id1", "name1", testutils.AutoAcceptPolicy())
	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Clusters))

	createCluster(t, ts, "id2", "name2", testutils.AutoAcceptPolicy())
	createCluster(t, ts, "id3", "name3", testutils.AutoAcceptPolicy())
	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Clusters))
	for _, cluster := range r.Clusters {
		assert.NotContains(t, cluster.String(), "PRIVATE")
	}
}
