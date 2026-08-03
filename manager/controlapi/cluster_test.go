package controlapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/ca"
	"github.com/moby/swarmkit/v2/ca/testutils"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/moby/swarmkit/v2/protobuf/ptypes"
	grpcutils "github.com/moby/swarmkit/v2/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	durationpb "google.golang.org/protobuf/types/known/durationpb"
)

func createClusterSpec(name string) *api.ClusterSpec {
	return &api.ClusterSpec{
		Annotations: &api.Annotations{
			Name: name,
		},
		CaConfig: &api.CAConfig{
			NodeCertExpiry: durationpb.New(ca.DefaultNodeCertExpiration),
		},
		// These were non-nullable embedded messages before the move to
		// protoc-gen-go, so the server could always reach through them.
		AcceptancePolicy: &api.AcceptancePolicy{},
		Orchestration:    &api.OrchestrationConfig{},
		Raft:             &api.RaftConfig{},
		Dispatcher:       &api.DispatcherConfig{},
		TaskDefaults:     &api.TaskDefaults{},
		EncryptionConfig: &api.EncryptionConfig{},
	}
}

func createClusterObj(id, name string, policy *api.AcceptancePolicy, rootCA *ca.RootCA) *api.Cluster {
	spec := createClusterSpec(name)
	spec.AcceptancePolicy = policy

	var key []byte
	if s, err := rootCA.Signer(); err == nil {
		key = s.Key
	}

	return &api.Cluster{
		Id:   id,
		Spec: spec,
		RootCa: &api.RootCA{
			CaCert:     rootCA.Certs,
			CaKey:      key,
			CaCertHash: rootCA.Digest.String(),
			JoinTokens: &api.JoinTokens{
				Worker:  ca.GenerateJoinToken(rootCA, false),
				Manager: ca.GenerateJoinToken(rootCA, false),
			},
		},
	}
}

func createCluster(t *testing.T, ts *testServer, id, name string, policy *api.AcceptancePolicy, rootCA *ca.RootCA) *api.Cluster {
	cluster := createClusterObj(id, name, policy, rootCA)
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
				Annotations: &api.Annotations{
					Name: store.DefaultClusterName,
				},
				CaConfig: &api.CAConfig{
					NodeCertExpiry: durationpb.New(29 * time.Minute),
				},
			},
			c: codes.InvalidArgument,
		},
		{
			spec: &api.ClusterSpec{
				Annotations: &api.Annotations{
					Name: store.DefaultClusterName,
				},
				Dispatcher: &api.DispatcherConfig{
					HeartbeatPeriod: durationpb.New(-29 * time.Minute),
				},
			},
			c: codes.InvalidArgument,
		},
		{
			spec: &api.ClusterSpec{
				Annotations: &api.Annotations{
					Name: "",
				},
			},
			c: codes.InvalidArgument,
		},
		{
			spec: &api.ClusterSpec{
				Annotations: &api.Annotations{
					Name: "blah",
				},
			},
			c: codes.InvalidArgument,
		},
	} {
		err := validateClusterSpec(bad.spec)
		assert.Error(t, err)
		assert.Equal(t, bad.c, grpcutils.ErrorCode(err))
	}

	for _, good := range []*api.ClusterSpec{
		createClusterSpec(store.DefaultClusterName),
	} {
		err := validateClusterSpec(good)
		assert.NoError(t, err)
	}

}

func TestGetCluster(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	_, err := ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpcutils.ErrorCode(err))

	_, err = ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{ClusterId: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpcutils.ErrorCode(err))

	cluster := createCluster(t, ts, "name", "name", &api.AcceptancePolicy{}, ts.Server.securityConfig.RootCA())
	r, err := ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{ClusterId: cluster.Id})
	assert.NoError(t, err)
	cluster.Meta.Version = r.Cluster.Meta.Version
	// Only public fields should be available
	assert.Equal(t, cluster.Id, r.Cluster.Id)
	assert.Equal(t, cluster.Meta, r.Cluster.Meta)
	assert.Equal(t, cluster.Spec, r.Cluster.Spec)
	assert.Equal(t, cluster.RootCa.GetCaCert(), r.Cluster.RootCa.GetCaCert())
	assert.Equal(t, cluster.RootCa.GetCaCertHash(), r.Cluster.RootCa.GetCaCertHash())
	// CAKey and network keys should be nil
	assert.Nil(t, r.Cluster.RootCa.GetCaKey())
	assert.Nil(t, r.Cluster.NetworkBootstrapKeys)
}

func TestGetClusterWithSecret(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	_, err := ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpcutils.ErrorCode(err))

	_, err = ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{ClusterId: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpcutils.ErrorCode(err))

	policy := &api.AcceptancePolicy{Policies: []*api.AcceptancePolicy_RoleAdmissionPolicy{{Secret: &api.AcceptancePolicy_RoleAdmissionPolicy_Secret{Data: []byte("secret")}}}}
	cluster := createCluster(t, ts, "name", "name", policy, ts.Server.securityConfig.RootCA())
	r, err := ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{ClusterId: cluster.Id})
	assert.NoError(t, err)
	cluster.Meta.Version = r.Cluster.Meta.Version
	assert.NotEqual(t, cluster, r.Cluster)
	assert.NotContains(t, r.Cluster.String(), "PRIVATE")
	// Assert on the fields redactClusters actually clears. The previous
	// assertion here looked for the literal "secret" in String(), which only
	// held because gogo rendered bytes fields as a []byte literal; the
	// official String() uses the protobuf text format, which prints them as
	// text. The acceptance policy secret is a bcrypt hash in practice and is
	// deliberately returned, as the assertion below shows.
	assert.Nil(t, r.Cluster.Spec.GetCaConfig().GetSigningCaKey())
	assert.Nil(t, r.Cluster.Spec.GetCaConfig().GetSigningCaCert())
	assert.Nil(t, r.Cluster.RootCa.GetCaKey())
	assert.NotNil(t, r.Cluster.Spec.GetAcceptancePolicy().GetPolicies()[0].Secret.Data)
}

func TestUpdateCluster(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	cluster := createCluster(t, ts, "name", store.DefaultClusterName, &api.AcceptancePolicy{}, ts.Server.securityConfig.RootCA())

	_, err := ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpcutils.ErrorCode(err))

	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{ClusterId: "invalid", Spec: cluster.Spec, ClusterVersion: &api.Version{}})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpcutils.ErrorCode(err))

	// No update options.
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{ClusterId: cluster.Id, Spec: cluster.Spec})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpcutils.ErrorCode(err))

	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{ClusterId: cluster.Id, Spec: cluster.Spec, ClusterVersion: cluster.Meta.Version})
	assert.NoError(t, err)

	r, err := ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{
		Filters: &api.ListClustersRequest_Filters{
			NamePrefixes: []string{store.DefaultClusterName},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, r.Clusters, 1)
	assert.Equal(t, cluster.GetSpec().GetAnnotations().GetName(), r.Clusters[0].GetSpec().GetAnnotations().GetName())
	assert.Len(t, r.Clusters[0].Spec.GetAcceptancePolicy().GetPolicies(), 0)

	r.Clusters[0].Spec.AcceptancePolicy = &api.AcceptancePolicy{Policies: []*api.AcceptancePolicy_RoleAdmissionPolicy{{Secret: &api.AcceptancePolicy_RoleAdmissionPolicy_Secret{Alg: "bcrypt", Data: []byte("secret")}}}}
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           r.Clusters[0].Spec,
		ClusterVersion: r.Clusters[0].Meta.Version,
	})
	assert.NoError(t, err)

	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{
		Filters: &api.ListClustersRequest_Filters{
			NamePrefixes: []string{store.DefaultClusterName},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, r.Clusters, 1)
	assert.Equal(t, cluster.GetSpec().GetAnnotations().GetName(), r.Clusters[0].GetSpec().GetAnnotations().GetName())
	assert.Len(t, r.Clusters[0].Spec.GetAcceptancePolicy().GetPolicies(), 1)

	r.Clusters[0].Spec.AcceptancePolicy = &api.AcceptancePolicy{Policies: []*api.AcceptancePolicy_RoleAdmissionPolicy{{Secret: &api.AcceptancePolicy_RoleAdmissionPolicy_Secret{Alg: "bcrypt", Data: []byte("secret")}}}}
	returnedCluster, err := ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           r.Clusters[0].Spec,
		ClusterVersion: r.Clusters[0].Meta.Version,
	})
	assert.NoError(t, err)
	assert.NotContains(t, returnedCluster.String(), "PRIVATE")
	// See the note in TestGetClusterWithSecret: assert on what redactClusters
	// actually clears rather than on how String() renders bytes fields.
	assert.Nil(t, returnedCluster.Cluster.Spec.GetCaConfig().GetSigningCaKey())
	assert.Nil(t, returnedCluster.Cluster.Spec.GetCaConfig().GetSigningCaCert())
	assert.Nil(t, returnedCluster.Cluster.RootCa.GetCaKey())
	assert.NotNil(t, returnedCluster.Cluster.Spec.GetAcceptancePolicy().GetPolicies()[0].Secret.Data)

	// Versioning.
	assert.NoError(t, err)
	version := returnedCluster.Cluster.Meta.Version

	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           r.Clusters[0].Spec,
		ClusterVersion: version,
	})
	assert.NoError(t, err)

	// Perform an update with the "old" version.
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           r.Clusters[0].Spec,
		ClusterVersion: version,
	})
	assert.Error(t, err)
}

func TestUpdateClusterRotateToken(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	cluster := createCluster(t, ts, "name", store.DefaultClusterName, &api.AcceptancePolicy{}, ts.Server.securityConfig.RootCA())

	r, err := ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{
		Filters: &api.ListClustersRequest_Filters{
			NamePrefixes: []string{store.DefaultClusterName},
		},
	})

	assert.NoError(t, err)
	assert.Len(t, r.Clusters, 1)
	workerToken := r.Clusters[0].RootCa.GetJoinTokens().GetWorker()
	managerToken := r.Clusters[0].RootCa.GetJoinTokens().GetManager()

	// Rotate worker token
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           cluster.Spec,
		ClusterVersion: cluster.Meta.Version,
		Rotation: &api.KeyRotation{
			WorkerJoinToken: true,
		},
	})
	assert.NoError(t, err)

	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{
		Filters: &api.ListClustersRequest_Filters{
			NamePrefixes: []string{store.DefaultClusterName},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, r.Clusters, 1)
	assert.NotEqual(t, workerToken, r.Clusters[0].RootCa.GetJoinTokens().GetWorker())
	assert.Equal(t, managerToken, r.Clusters[0].RootCa.GetJoinTokens().GetManager())
	workerToken = r.Clusters[0].RootCa.GetJoinTokens().GetWorker()

	// Rotate manager token
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           cluster.Spec,
		ClusterVersion: r.Clusters[0].Meta.Version,
		Rotation: &api.KeyRotation{
			ManagerJoinToken: true,
		},
	})
	assert.NoError(t, err)

	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{
		Filters: &api.ListClustersRequest_Filters{
			NamePrefixes: []string{store.DefaultClusterName},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, r.Clusters, 1)
	assert.Equal(t, workerToken, r.Clusters[0].RootCa.GetJoinTokens().GetWorker())
	assert.NotEqual(t, managerToken, r.Clusters[0].RootCa.GetJoinTokens().GetManager())
	managerToken = r.Clusters[0].RootCa.GetJoinTokens().GetManager()

	// Rotate both tokens
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           cluster.Spec,
		ClusterVersion: r.Clusters[0].Meta.Version,
		Rotation: &api.KeyRotation{
			WorkerJoinToken:  true,
			ManagerJoinToken: true,
		},
	})
	assert.NoError(t, err)

	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{
		Filters: &api.ListClustersRequest_Filters{
			NamePrefixes: []string{store.DefaultClusterName},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, r.Clusters, 1)
	assert.NotEqual(t, workerToken, r.Clusters[0].RootCa.GetJoinTokens().GetWorker())
	assert.NotEqual(t, managerToken, r.Clusters[0].RootCa.GetJoinTokens().GetManager())
}

func TestUpdateClusterRotateUnlockKey(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	// create a cluster with extra encryption keys, to make sure they exist
	cluster := createClusterObj("id", store.DefaultClusterName, &api.AcceptancePolicy{}, ts.Server.securityConfig.RootCA())
	expected := make(map[string]*api.EncryptionKey)
	for i := 1; i <= 2; i++ {
		value := fmt.Sprintf("fake%d", i)
		expected[value] = &api.EncryptionKey{Subsystem: value, Key: []byte(value)}
		cluster.UnlockKeys = append(cluster.UnlockKeys, expected[value])
	}
	require.NoError(t, ts.Store.Update(func(tx store.Tx) error {
		return store.CreateCluster(tx, cluster)
	}))

	// we have to get the key from the memory store, since the cluster returned by the API is redacted
	getManagerKey := func() (managerKey *api.EncryptionKey) {
		ts.Store.View(func(tx store.ReadTx) {
			viewCluster := store.GetCluster(tx, cluster.Id)
			// no matter whether there's a manager key or not, the other keys should not have been affected
			foundKeys := make(map[string]*api.EncryptionKey)
			for _, eKey := range viewCluster.UnlockKeys {
				foundKeys[eKey.Subsystem] = eKey
			}
			for v, key := range expected {
				foundKey, ok := foundKeys[v]
				require.True(t, ok)
				require.Equal(t, key, foundKey)
			}
			managerKey = foundKeys[ca.ManagerRole]
		})
		return
	}

	validateListResult := func(expectedLocked bool) *api.Version {
		r, err := ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{
			Filters: &api.ListClustersRequest_Filters{
				NamePrefixes: []string{store.DefaultClusterName},
			},
		})

		require.NoError(t, err)
		require.Len(t, r.Clusters, 1)
		require.Equal(t, expectedLocked, r.Clusters[0].Spec.GetEncryptionConfig().GetAutoLockManagers())
		require.Nil(t, r.Clusters[0].UnlockKeys) // redacted

		return r.Clusters[0].Meta.Version
	}

	// we start off with manager autolocking turned off
	version := validateListResult(false)
	require.Nil(t, getManagerKey())

	// Rotate unlock key without turning auto-lock on - key should still be nil
	_, err := ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           cluster.Spec,
		ClusterVersion: version,
		Rotation: &api.KeyRotation{
			ManagerUnlockKey: true,
		},
	})
	require.NoError(t, err)
	version = validateListResult(false)
	require.Nil(t, getManagerKey())

	// Enable auto-lock only, no rotation boolean
	spec := cluster.Spec.Copy()
	spec.EncryptionConfig.AutoLockManagers = true
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           spec,
		ClusterVersion: version,
	})
	require.NoError(t, err)
	version = validateListResult(true)
	managerKey := getManagerKey()
	require.NotNil(t, managerKey)

	// Rotate the manager key
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           spec,
		ClusterVersion: version,
		Rotation: &api.KeyRotation{
			ManagerUnlockKey: true,
		},
	})
	require.NoError(t, err)
	version = validateListResult(true)
	newManagerKey := getManagerKey()
	require.NotNil(t, managerKey)
	require.NotEqual(t, managerKey, newManagerKey)
	managerKey = newManagerKey

	// Just update the cluster without modifying unlock keys
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           spec,
		ClusterVersion: version,
	})
	require.NoError(t, err)
	version = validateListResult(true)
	newManagerKey = getManagerKey()
	require.Equal(t, managerKey, newManagerKey)

	// Disable auto lock
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           cluster.Spec, // set back to original spec
		ClusterVersion: version,
		Rotation: &api.KeyRotation{
			ManagerUnlockKey: true, // this will be ignored because we disable the auto-lock
		},
	})
	require.NoError(t, err)
	validateListResult(false)
	require.Nil(t, getManagerKey())
}

// root rotation tests have already been covered by ca_rotation_test.go - this test only makes sure that the function tested in those
// tests is actually called by `UpdateCluster`, and that the results of GetCluster and ListCluster have the CA keys
// and the spec key and cert redacted
func TestUpdateClusterRootRotation(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	cluster := createCluster(t, ts, "id", store.DefaultClusterName, &api.AcceptancePolicy{}, ts.Server.securityConfig.RootCA())
	response, err := ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{ClusterId: cluster.Id})
	require.NoError(t, err)
	require.NotNil(t, response.Cluster)
	cluster = response.Cluster

	updatedSpec := cluster.Spec.Copy()
	updatedSpec.CaConfig.SigningCaCert = testutils.ECDSA256SHA256Cert
	updatedSpec.CaConfig.SigningCaKey = testutils.ECDSA256Key
	updatedSpec.CaConfig.ForceRotate = 5

	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           updatedSpec,
		ClusterVersion: cluster.Meta.Version,
	})
	require.NoError(t, err)

	checkCluster := func() *api.Cluster {
		response, err = ts.Client.GetCluster(context.Background(), &api.GetClusterRequest{ClusterId: cluster.Id})
		require.NoError(t, err)
		require.NotNil(t, response.Cluster)

		listResponse, err := ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
		require.NoError(t, err)
		require.Len(t, listResponse.Clusters, 1)

		require.Equal(t, response.Cluster, listResponse.Clusters[0])

		c := response.Cluster
		require.NotNil(t, c.RootCa.GetRootRotation())

		// check that all keys are redacted, and that the spec signing cert is also redacted (not because
		// the cert is a secret, but because that makes it easier to get-and-update)
		require.Len(t, c.RootCa.GetCaKey(), 0)
		require.Len(t, c.RootCa.GetRootRotation().GetCaKey(), 0)
		require.Len(t, c.Spec.GetCaConfig().GetSigningCaKey(), 0)
		require.Len(t, c.Spec.GetCaConfig().GetSigningCaCert(), 0)

		return c
	}

	getUnredactedRootCA := func() (rootCA *api.RootCA) {
		ts.Store.View(func(tx store.ReadTx) {
			c := store.GetCluster(tx, cluster.Id)
			require.NotNil(t, c)
			rootCA = c.RootCa
		})
		return
	}

	cluster = checkCluster()
	unredactedRootCA := getUnredactedRootCA()

	// update something else, but make sure this doesn't the root CA rotation doesn't change
	updatedSpec = cluster.Spec.Copy()
	updatedSpec.CaConfig.NodeCertExpiry = durationpb.New(time.Hour)
	_, err = ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId:      cluster.Id,
		Spec:           updatedSpec,
		ClusterVersion: cluster.Meta.Version,
	})
	require.NoError(t, err)

	updatedCluster := checkCluster()
	require.NotEqual(t, cluster.Spec.GetCaConfig().GetNodeCertExpiry(), updatedCluster.Spec.GetCaConfig().GetNodeCertExpiry())
	updatedUnredactedRootCA := getUnredactedRootCA()

	require.Equal(t, unredactedRootCA, updatedUnredactedRootCA)
}

func TestListClusters(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	r, err := ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Empty(t, r.Clusters)

	createCluster(t, ts, "id1", "name1", &api.AcceptancePolicy{}, ts.Server.securityConfig.RootCA())
	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Clusters))

	createCluster(t, ts, "id2", "name2", &api.AcceptancePolicy{}, ts.Server.securityConfig.RootCA())
	createCluster(t, ts, "id3", "name3", &api.AcceptancePolicy{}, ts.Server.securityConfig.RootCA())
	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Clusters))
}

func TestListClustersWithSecrets(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	r, err := ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Empty(t, r.Clusters)

	policy := &api.AcceptancePolicy{Policies: []*api.AcceptancePolicy_RoleAdmissionPolicy{{Secret: &api.AcceptancePolicy_RoleAdmissionPolicy_Secret{Alg: "bcrypt", Data: []byte("secret")}}}}

	createCluster(t, ts, "id1", "name1", policy, ts.Server.securityConfig.RootCA())
	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Clusters))

	createCluster(t, ts, "id2", "name2", policy, ts.Server.securityConfig.RootCA())
	createCluster(t, ts, "id3", "name3", policy, ts.Server.securityConfig.RootCA())
	r, err = ts.Client.ListClusters(context.Background(), &api.ListClustersRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Clusters))
	for _, cluster := range r.Clusters {
		assert.NotContains(t, cluster.String(), policy.Policies[0].Secret)
		assert.NotContains(t, cluster.String(), "PRIVATE")
		assert.NotNil(t, cluster.Spec.GetAcceptancePolicy().GetPolicies()[0].Secret.Data)
	}
}

func TestExpireBlacklistedCerts(t *testing.T) {
	now := time.Now()

	longAgo := now.Add(-24 * time.Hour * 1000)
	justBeforeGrace := now.Add(-expiredCertGrace - 5*time.Minute)
	justAfterGrace := now.Add(-expiredCertGrace + 5*time.Minute)
	future := now.Add(time.Hour)

	cluster := &api.Cluster{
		BlacklistedCertificates: map[string]*api.BlacklistedCertificate{
			"longAgo":         {Expiry: ptypes.MustTimestampProto(longAgo)},
			"justBeforeGrace": {Expiry: ptypes.MustTimestampProto(justBeforeGrace)},
			"justAfterGrace":  {Expiry: ptypes.MustTimestampProto(justAfterGrace)},
			"future":          {Expiry: ptypes.MustTimestampProto(future)},
		},
	}

	expireBlacklistedCerts(cluster)

	assert.Len(t, cluster.BlacklistedCertificates, 2)

	_, hasJustAfterGrace := cluster.BlacklistedCertificates["justAfterGrace"]
	assert.True(t, hasJustAfterGrace)

	_, hasFuture := cluster.BlacklistedCertificates["future"]
	assert.True(t, hasFuture)
}

// TestUpdateClusterBackfillsSpec verifies that a ClusterSpec accepted without
// its submessages is stored with all of them present. They were non-nullable
// before the migration to the standard protobuf runtime, and consumers (such
// as the raft config check on manager startup) dereference them directly.
func TestUpdateClusterBackfillsSpec(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	cluster := createCluster(t, ts, "id", store.DefaultClusterName, &api.AcceptancePolicy{}, ts.Server.securityConfig.RootCA())

	_, err := ts.Client.UpdateCluster(context.Background(), &api.UpdateClusterRequest{
		ClusterId: cluster.Id,
		Spec: &api.ClusterSpec{
			Annotations: &api.Annotations{Name: store.DefaultClusterName},
		},
		ClusterVersion: cluster.Meta.Version,
	})
	require.NoError(t, err)

	var stored *api.Cluster
	ts.Store.View(func(tx store.ReadTx) {
		stored = store.GetCluster(tx, cluster.Id)
	})
	require.NotNil(t, stored)

	spec := stored.Spec
	require.NotNil(t, spec, "stored cluster must have a Spec")
	assert.NotNil(t, spec.Annotations, "stored cluster must have Spec.Annotations")
	assert.NotNil(t, spec.AcceptancePolicy, "stored cluster must have Spec.AcceptancePolicy")
	assert.NotNil(t, spec.Orchestration, "stored cluster must have Spec.Orchestration")
	assert.NotNil(t, spec.Raft, "stored cluster must have Spec.Raft")
	assert.NotNil(t, spec.Dispatcher, "stored cluster must have Spec.Dispatcher")
	assert.NotNil(t, spec.CaConfig, "stored cluster must have Spec.CaConfig")
	assert.NotNil(t, spec.TaskDefaults, "stored cluster must have Spec.TaskDefaults")
	assert.NotNil(t, spec.EncryptionConfig, "stored cluster must have Spec.EncryptionConfig")
}
