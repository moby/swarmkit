package controlapi

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	gogotypes "github.com/gogo/protobuf/types"
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/identity"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/moby/swarmkit/v2/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func createGenericSpec(name, runtime string) *api.ServiceSpec {
	spec := createSpec(name, runtime, 0)
	spec.Task.Runtime = &api.TaskSpec_Generic{
		Generic: &api.GenericRuntimeSpec{
			Kind: runtime,
			Payload: &gogotypes.Any{
				TypeUrl: "com.docker.custom.runtime",
				Value:   []byte{0},
			},
		},
	}
	return spec
}

func createSpec(name, image string, instances uint64) *api.ServiceSpec {
	return &api.ServiceSpec{
		Annotations: api.Annotations{
			Name: name,
			Labels: map[string]string{
				"common": "yes",
				"unique": name,
			},
		},
		Task: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image: image,
				},
			},
		},
		Mode: &api.ServiceSpec_Replicated{
			Replicated: &api.ReplicatedService{
				Replicas: instances,
			},
		},
	}
}

func createSpecWithDuplicateMounts(name string) *api.ServiceSpec {
	service := createSpec("", "image", 1)
	mounts := []api.Mount{
		{
			Target: "/foo",
			Source: "/mnt/mount1",
		},
		{
			Target: "/foo",
			Source: "/mnt/mount2",
		},
	}

	service.Task.GetContainer().Mounts = mounts

	return service
}

func createSpecWithHostnameTemplate(serviceName, hostnameTmpl string) *api.ServiceSpec {
	service := createSpec(serviceName, "image", 1)
	service.Task.GetContainer().Hostname = hostnameTmpl
	return service
}

func createSecret(t *testing.T, ts *testServer, secretName, target string) *api.SecretReference {
	secretSpec := createSecretSpec(secretName, []byte(secretName), nil)
	secret := &api.Secret{
		ID:   fmt.Sprintf("ID%v", secretName),
		Spec: *secretSpec,
	}
	err := ts.Store.Update(func(tx store.Tx) error {
		return store.CreateSecret(tx, secret)
	})
	require.NoError(t, err)

	return &api.SecretReference{
		SecretName: secret.Spec.Annotations.Name,
		SecretID:   secret.ID,
		Target: &api.SecretReference_File{
			File: &api.FileTarget{
				Name: target,
				UID:  "0",
				GID:  "0",
				Mode: 0666,
			},
		},
	}
}

func createServiceSpecWithSecrets(serviceName string, secretRefs ...*api.SecretReference) *api.ServiceSpec {
	service := createSpec(serviceName, fmt.Sprintf("image%v", serviceName), 1)
	service.Task.GetContainer().Secrets = secretRefs

	return service
}

func createConfig(t *testing.T, ts *testServer, configName, target string) *api.ConfigReference {
	configSpec := createConfigSpec(configName, []byte(configName), nil)
	config := &api.Config{
		ID:   fmt.Sprintf("ID%v", configName),
		Spec: *configSpec,
	}
	err := ts.Store.Update(func(tx store.Tx) error {
		return store.CreateConfig(tx, config)
	})
	require.NoError(t, err)

	return &api.ConfigReference{
		ConfigName: config.Spec.Annotations.Name,
		ConfigID:   config.ID,
		Target: &api.ConfigReference_File{
			File: &api.FileTarget{
				Name: target,
				UID:  "0",
				GID:  "0",
				Mode: 0666,
			},
		},
	}
}

func createServiceSpecWithConfigs(serviceName string, configRefs ...*api.ConfigReference) *api.ServiceSpec {
	service := createSpec(serviceName, fmt.Sprintf("image%v", serviceName), 1)
	service.Task.GetContainer().Configs = configRefs

	return service
}

func createService(t *testing.T, ts *testServer, name, image string, instances uint64) *api.Service {
	spec := createSpec(name, image, instances)
	r, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)
	return r.Service
}

func createGenericService(t *testing.T, ts *testServer, name, runtime string) *api.Service {
	spec := createGenericSpec(name, runtime)
	r, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)
	return r.Service
}

func getIngressTargetID(t *testing.T, ts *testServer) string {
	rsp, err := ts.Client.ListNetworks(context.Background(), &api.ListNetworksRequest{})
	require.NoError(t, err)
	for _, n := range rsp.Networks {
		if n.Spec.Ingress {
			return n.ID
		}
	}
	t.Fatal("unable to find ingress")
	return ""
}

func TestValidateResources(t *testing.T) {
	bad := []*api.Resources{
		{MemoryBytes: 1},
		{NanoCPUs: 42},
	}

	good := []*api.Resources{
		{MemoryBytes: 4096 * 1024 * 1024},
		{NanoCPUs: 1e9},
	}

	for _, b := range bad {
		err := validateResources(b)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
	}

	for _, g := range good {
		require.NoError(t, validateResources(g))
	}
}

func TestValidateResourceRequirements(t *testing.T) {
	bad := []*api.ResourceRequirements{
		{Limits: &api.Resources{MemoryBytes: 1}},
		{Reservations: &api.Resources{MemoryBytes: 1}},
	}
	good := []*api.ResourceRequirements{
		{Limits: &api.Resources{NanoCPUs: 1e9}},
		{Reservations: &api.Resources{NanoCPUs: 1e9}},
	}
	for _, b := range bad {
		err := validateResourceRequirements(b)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
	}

	for _, g := range good {
		require.NoError(t, validateResourceRequirements(g))
	}
}

func TestValidateMode(t *testing.T) {
	negative := -4
	bad := []*api.ServiceSpec{
		// -4 jammed into the replicas field, underflowing the uint64
		{Mode: &api.ServiceSpec_Replicated{Replicated: &api.ReplicatedService{Replicas: uint64(negative)}}},
		{Mode: &api.ServiceSpec_ReplicatedJob{ReplicatedJob: &api.ReplicatedJob{MaxConcurrent: uint64(negative)}}},
		{Mode: &api.ServiceSpec_ReplicatedJob{ReplicatedJob: &api.ReplicatedJob{TotalCompletions: uint64(negative)}}},
		{},
	}

	good := []*api.ServiceSpec{
		{Mode: &api.ServiceSpec_Replicated{Replicated: &api.ReplicatedService{Replicas: 2}}},
		{Mode: &api.ServiceSpec_Global{}},
		{
			Mode: &api.ServiceSpec_ReplicatedJob{
				ReplicatedJob: &api.ReplicatedJob{
					MaxConcurrent: 3, TotalCompletions: 9,
				},
			},
		},
		{Mode: &api.ServiceSpec_GlobalJob{}},
	}

	for _, b := range bad {
		err := validateMode(b)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
	}

	for _, g := range good {
		err := validateMode(g)
		require.NoError(t, err)
	}
}

func TestValidateTaskSpec(t *testing.T) {
	type badSource struct {
		s api.TaskSpec
		c codes.Code
	}

	for _, bad := range []badSource{
		{
			s: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{},
				},
			},
			c: codes.InvalidArgument,
		},
		{
			s: api.TaskSpec{
				Runtime: &api.TaskSpec_Attachment{
					Attachment: &api.NetworkAttachmentSpec{},
				},
			},
			c: codes.Unimplemented,
		},
		{
			s: createSpec("", "", 0).Task,
			c: codes.InvalidArgument,
		},
		{
			s: createSpec("", "busybox###", 0).Task,
			c: codes.InvalidArgument,
		},
		{
			s: createGenericSpec("name", "").Task,
			c: codes.InvalidArgument,
		},
		{
			s: createGenericSpec("name", "c").Task,
			c: codes.InvalidArgument,
		},
		{
			s: createSpecWithDuplicateMounts("test").Task,
			c: codes.InvalidArgument,
		},
		{
			s: createSpecWithHostnameTemplate("", "{{.Nothing.here}}").Task,
			c: codes.InvalidArgument,
		},
	} {
		err := validateTaskSpec(bad.s)
		require.Error(t, err)
		assert.Equal(t, bad.c, testutils.ErrorCode(err))
	}

	for _, good := range []api.TaskSpec{
		createSpec("", "image", 0).Task,
		createGenericSpec("", "custom").Task,
		createSpecWithHostnameTemplate("service", "{{.Service.Name}}-{{.Task.Slot}}").Task,
	} {
		err := validateTaskSpec(good)
		require.NoError(t, err)
	}
}

func TestValidateContainerSpec(t *testing.T) {
	type BadSpec struct {
		spec api.TaskSpec
		c    codes.Code
	}

	bad1 := api.TaskSpec{
		Runtime: &api.TaskSpec_Container{
			Container: &api.ContainerSpec{
				Image: "", // image name should not be empty
			},
		},
	}

	bad2 := api.TaskSpec{
		Runtime: &api.TaskSpec_Container{
			Container: &api.ContainerSpec{
				Image: "image",
				Mounts: []api.Mount{
					{
						Type:   api.Mount_MountType(0),
						Source: "/data",
						Target: "/data",
					},
					{
						Type:   api.Mount_MountType(0),
						Source: "/data2",
						Target: "/data", // duplicate mount point
					},
				},
			},
		},
	}

	bad3 := api.TaskSpec{
		Runtime: &api.TaskSpec_Container{
			Container: &api.ContainerSpec{
				Image: "image",
				Healthcheck: &api.HealthConfig{
					Test:          []string{"curl 127.0.0.1:3000"},
					Interval:      gogotypes.DurationProto(time.Duration(-1 * time.Second)), // invalid negative duration
					Timeout:       gogotypes.DurationProto(time.Duration(-1 * time.Second)), // invalid negative duration
					Retries:       -1,                                                       // invalid negative integer
					StartPeriod:   gogotypes.DurationProto(time.Duration(-1 * time.Second)), // invalid negative duration
					StartInterval: gogotypes.DurationProto(time.Duration(-1 * time.Second)), // invalid negative duration
				},
			},
		},
	}

	for _, bad := range []BadSpec{
		{
			spec: bad1,
			c:    codes.InvalidArgument,
		},
		{
			spec: bad2,
			c:    codes.InvalidArgument,
		},
		{
			spec: bad3,
			c:    codes.InvalidArgument,
		},
	} {
		err := validateContainerSpec(bad.spec)
		require.Error(t, err)
		assert.Equal(t, bad.c, testutils.ErrorCode(err), testutils.ErrorDesc(err))
	}

	good1 := api.TaskSpec{
		Runtime: &api.TaskSpec_Container{
			Container: &api.ContainerSpec{
				Image: "image",
				Mounts: []api.Mount{
					{
						Type:   api.Mount_MountType(0),
						Source: "/data",
						Target: "/data",
					},
					{
						Type:   api.Mount_MountType(0),
						Source: "/data2",
						Target: "/data2",
					},
				},
				Healthcheck: &api.HealthConfig{
					Test:          []string{"curl 127.0.0.1:3000"},
					Interval:      gogotypes.DurationProto(time.Duration(1 * time.Second)),
					Timeout:       gogotypes.DurationProto(time.Duration(3 * time.Second)),
					Retries:       5,
					StartPeriod:   gogotypes.DurationProto(time.Duration(1 * time.Second)),
					StartInterval: gogotypes.DurationProto(time.Duration(1 * time.Second)),
				},
			},
		},
	}

	for _, good := range []api.TaskSpec{good1} {
		err := validateContainerSpec(good)
		require.NoError(t, err)
	}
}

func TestValidateServiceSpec(t *testing.T) {
	type BadServiceSpec struct {
		spec *api.ServiceSpec
		c    codes.Code
	}

	for _, bad := range []BadServiceSpec{
		{
			spec: nil,
			c:    codes.InvalidArgument,
		},
		{
			spec: &api.ServiceSpec{Annotations: api.Annotations{Name: "name"}},
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec("", "", 1),
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec("name", "", 1),
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec("", "image", 1),
			c:    codes.InvalidArgument,
		},
		{
			spec: createSpec(strings.Repeat("longname", 8), "image", 1),
			c:    codes.InvalidArgument,
		},
	} {
		err := validateServiceSpec(bad.spec)
		require.Error(t, err)
		assert.Equal(t, bad.c, testutils.ErrorCode(err), testutils.ErrorDesc(err))
	}

	for _, good := range []*api.ServiceSpec{
		createSpec("name", "image", 1),
	} {
		err := validateServiceSpec(good)
		require.NoError(t, err)
	}
}

// TestValidateServiceSpecJobsDifference is different from
// TestValidateServiceSpec in that it checks that job-mode services are
// validated differently from regular services.
func TestValidateServiceSpecJobsDifference(t *testing.T) {
	// correctly formed spec should be valid
	cannedSpec := createSpec("name", "image", 1)
	err := validateServiceSpec(cannedSpec)
	require.NoError(t, err)

	// Replicated job should not be allowed to have update config
	specReplicatedJobUpdate := cannedSpec.Copy()
	specReplicatedJobUpdate.Mode = &api.ServiceSpec_ReplicatedJob{
		ReplicatedJob: &api.ReplicatedJob{},
	}
	specReplicatedJobUpdate.Update = &api.UpdateConfig{}
	err = validateServiceSpec(specReplicatedJobUpdate)
	require.Error(t, err)

	specReplicatedJobNoUpdate := specReplicatedJobUpdate.Copy()
	specReplicatedJobNoUpdate.Update = nil
	err = validateServiceSpec(specReplicatedJobNoUpdate)
	require.NoError(t, err)

	// Global job should not be allowed to have update config
	specGlobalJobUpdate := cannedSpec.Copy()
	specGlobalJobUpdate.Mode = &api.ServiceSpec_GlobalJob{
		GlobalJob: &api.GlobalJob{},
	}
	specGlobalJobUpdate.Update = &api.UpdateConfig{}
	err = validateServiceSpec(specGlobalJobUpdate)
	require.Error(t, err)

	specGlobalJobNoUpdate := specGlobalJobUpdate.Copy()
	specGlobalJobNoUpdate.Update = nil
	err = validateServiceSpec(specReplicatedJobNoUpdate)
	require.NoError(t, err)

	// Replicated service should be allowed to have update config, which should
	// be verified for correctness
	replicatedServiceBrokenUpdate := cannedSpec.Copy()
	replicatedServiceBrokenUpdate.Update = &api.UpdateConfig{
		Delay: -1 * time.Second,
	}
	err = validateServiceSpec(replicatedServiceBrokenUpdate)
	require.Error(t, err)

	replicatedServiceCorrectUpdate := replicatedServiceBrokenUpdate.Copy()
	replicatedServiceCorrectUpdate.Update.Delay = time.Second
	err = validateServiceSpec(replicatedServiceCorrectUpdate)
	require.NoError(t, err)

	// Global service should be allowed to have update config, which should be
	// verified for correctness
	globalServiceBrokenUpdate := replicatedServiceBrokenUpdate.Copy()
	globalServiceBrokenUpdate.Mode = &api.ServiceSpec_Global{
		Global: &api.GlobalService{},
	}
	err = validateServiceSpec(globalServiceBrokenUpdate)
	require.Error(t, err)

	globalServiceCorrectUpdate := globalServiceBrokenUpdate.Copy()
	globalServiceCorrectUpdate.Update.Delay = time.Second
	err = validateServiceSpec(globalServiceCorrectUpdate)
}

func TestValidateRestartPolicy(t *testing.T) {
	bad := []*api.RestartPolicy{
		{
			Delay:  gogotypes.DurationProto(time.Duration(-1 * time.Second)),
			Window: gogotypes.DurationProto(time.Duration(-1 * time.Second)),
		},
		{
			Delay:  gogotypes.DurationProto(time.Duration(20 * time.Second)),
			Window: gogotypes.DurationProto(time.Duration(-4 * time.Second)),
		},
	}

	good := []*api.RestartPolicy{
		{
			Delay:  gogotypes.DurationProto(time.Duration(10 * time.Second)),
			Window: gogotypes.DurationProto(time.Duration(1 * time.Second)),
		},
	}

	for _, b := range bad {
		err := validateRestartPolicy(b)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
	}

	for _, g := range good {
		require.NoError(t, validateRestartPolicy(g))
	}
}

func TestValidateUpdate(t *testing.T) {
	bad := []*api.UpdateConfig{
		{Delay: -1 * time.Second},
		{Delay: -1000 * time.Second},
		{Monitor: gogotypes.DurationProto(time.Duration(-1 * time.Second))},
		{Monitor: gogotypes.DurationProto(time.Duration(-1000 * time.Second))},
		{MaxFailureRatio: -0.1},
		{MaxFailureRatio: 1.1},
	}

	good := []*api.UpdateConfig{
		{Delay: time.Second},
		{Monitor: gogotypes.DurationProto(time.Duration(time.Second))},
		{MaxFailureRatio: 0.5},
	}

	for _, b := range bad {
		err := validateUpdate(b)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
	}

	for _, g := range good {
		require.NoError(t, validateUpdate(g))
	}
}

func TestCreateService(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	_, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	spec := createSpec("name", "image", 1)
	r, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)
	assert.NotEmpty(t, r.Service.ID)

	// test port conflicts
	spec = createSpec("name2", "image", 1)
	spec.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9000), TargetPort: uint32(9000), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)
	assert.NotEmpty(t, r.Service.ID)

	spec2 := createSpec("name3", "image", 1)
	spec2.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9000), TargetPort: uint32(9000), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec2})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	// test no port conflicts when no publish port is specified
	spec3 := createSpec("name4", "image", 1)
	spec3.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{TargetPort: uint32(9000), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec3})
	require.NoError(t, err)
	assert.NotEmpty(t, r.Service.ID)
	spec4 := createSpec("name5", "image", 1)
	spec4.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{TargetPort: uint32(9001), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec4})
	require.NoError(t, err)

	// ensure no port conflict when different protocols are used
	spec = createSpec("name6", "image", 1)
	spec.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9100), TargetPort: uint32(9100), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)
	assert.NotEmpty(t, r.Service.ID)

	spec2 = createSpec("name7", "image", 1)
	spec2.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9100), TargetPort: uint32(9100), Protocol: api.PortConfig_Protocol(api.ProtocolUDP)},
	}}
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec2})
	require.NoError(t, err)

	// ensure no port conflict when host ports overlap
	spec = createSpec("name8", "image", 1)
	spec.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishMode: api.PublishModeHost, PublishedPort: uint32(9101), TargetPort: uint32(9101), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)
	assert.NotEmpty(t, r.Service.ID)

	spec2 = createSpec("name9", "image", 1)
	spec2.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishMode: api.PublishModeHost, PublishedPort: uint32(9101), TargetPort: uint32(9101), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec2})
	require.NoError(t, err)

	// ensure port conflict when host ports overlaps with ingress port (host port first)
	spec = createSpec("name10", "image", 1)
	spec.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishMode: api.PublishModeHost, PublishedPort: uint32(9102), TargetPort: uint32(9102), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)
	assert.NotEmpty(t, r.Service.ID)

	spec2 = createSpec("name11", "image", 1)
	spec2.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishMode: api.PublishModeIngress, PublishedPort: uint32(9102), TargetPort: uint32(9102), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec2})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	// ensure port conflict when host ports overlaps with ingress port (ingress port first)
	spec = createSpec("name12", "image", 1)
	spec.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishMode: api.PublishModeIngress, PublishedPort: uint32(9103), TargetPort: uint32(9103), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)
	assert.NotEmpty(t, r.Service.ID)

	spec2 = createSpec("name13", "image", 1)
	spec2.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishMode: api.PublishModeHost, PublishedPort: uint32(9103), TargetPort: uint32(9103), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec2})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	// ingress network cannot be attached explicitly
	spec = createSpec("name14", "image", 1)
	spec.Task.Networks = []*api.NetworkAttachmentConfig{{Target: getIngressTargetID(t, ts)}}
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	spec = createSpec("notunique", "image", 1)
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)

	r, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, testutils.ErrorCode(err))

	// Make sure the error contains "name conflicts with an existing object" for
	// backward-compatibility with older clients doing string-matching...
	assert.Contains(t, err.Error(), "name conflicts with an existing object")
}

func TestSecretValidation(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	// test creating service with a secret that doesn't exist fails
	secretRef := createSecret(t, ts, "secret", "secret.txt")
	secretRef.SecretID = "404"
	secretRef.SecretName = "404"
	serviceSpec := createServiceSpecWithSecrets("service", secretRef)
	_, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	// test creating service with a secretRef that has an existing secret
	// but mismatched SecretName fails.
	secretRef1 := createSecret(t, ts, "secret1", "secret1.txt")
	secretRef1.SecretName = "secret2"
	serviceSpec = createServiceSpecWithSecrets("service1", secretRef1)
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	// test secret target conflicts
	secretRef2 := createSecret(t, ts, "secret2", "secret2.txt")
	secretRef3 := createSecret(t, ts, "secret3", "secret2.txt")
	serviceSpec = createServiceSpecWithSecrets("service2", secretRef2, secretRef3)
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	// test secret target conflicts with same secret and two references
	secretRef3.SecretID = secretRef2.SecretID
	secretRef3.SecretName = secretRef2.SecretName
	serviceSpec = createServiceSpecWithSecrets("service3", secretRef2, secretRef3)
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	// test two different secretReferences with using the same secret
	secretRef5 := secretRef2.Copy()
	secretRef5.Target = &api.SecretReference_File{
		File: &api.FileTarget{
			Name: "different-target",
		},
	}

	serviceSpec = createServiceSpecWithSecrets("service4", secretRef2, secretRef5)
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
	require.NoError(t, err)

	// test secret References with invalid filenames
	secretRefBlank := createSecret(t, ts, "", "")

	serviceSpec = createServiceSpecWithSecrets("invalid-blank", secretRefBlank)
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	// Test secret References with valid filenames
	// Note: "../secretfile.txt", "../../secretfile.txt" will be rejected
	// by the executor, but controlapi presently doesn't reject those names.
	// Such validation would be platform-specific.
	validFileNames := []string{"file.txt", ".file.txt", "_file-txt_.txt", "../secretfile.txt", "../../secretfile.txt", "file../.txt", "subdir/file.txt", "/file.txt"}
	for i, validName := range validFileNames {
		secretRef := createSecret(t, ts, validName, validName)

		serviceSpec = createServiceSpecWithSecrets(fmt.Sprintf("valid%v", i), secretRef)
		_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
		require.NoError(t, err)
	}

	// test secret target conflicts on update
	serviceSpec1 := createServiceSpecWithSecrets("service5", secretRef2, secretRef3)
	// Copy this service, but delete the secrets for creation
	serviceSpec2 := serviceSpec1.Copy()
	serviceSpec2.Task.GetContainer().Secrets = nil
	rs, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec2})
	require.NoError(t, err)

	// Attempt to update to the originally intended (conflicting) spec
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      rs.Service.ID,
		Spec:           serviceSpec1,
		ServiceVersion: &rs.Service.Meta.Version,
	})
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
}

func TestConfigValidation(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	// test creating service with a config that doesn't exist fails
	configRef := createConfig(t, ts, "config", "config.txt")
	configRef.ConfigID = "404"
	configRef.ConfigName = "404"
	serviceSpec := createServiceSpecWithConfigs("service", configRef)
	_, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	// test creating service with a configRef that has an existing config
	// but mismatched ConfigName fails.
	configRef1 := createConfig(t, ts, "config1", "config1.txt")
	configRef1.ConfigName = "config2"
	serviceSpec = createServiceSpecWithConfigs("service1", configRef1)
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	// test config target conflicts
	configRef2 := createConfig(t, ts, "config2", "config2.txt")
	configRef3 := createConfig(t, ts, "config3", "config2.txt")
	serviceSpec = createServiceSpecWithConfigs("service2", configRef2, configRef3)
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	// test config target conflicts with same config and two references
	configRef3.ConfigID = configRef2.ConfigID
	configRef3.ConfigName = configRef2.ConfigName
	serviceSpec = createServiceSpecWithConfigs("service3", configRef2, configRef3)
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	// test two different configReferences with using the same config
	configRef5 := configRef2.Copy()
	configRef5.Target = &api.ConfigReference_File{
		File: &api.FileTarget{
			Name: "different-target",
		},
	}

	serviceSpec = createServiceSpecWithConfigs("service4", configRef2, configRef5)
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
	require.NoError(t, err)

	// Test config References with valid filenames
	// TODO(aaronl): Should some of these be disallowed? How can we deal
	// with Windows-style paths on a Linux manager or vice versa?
	validFileNames := []string{"../configfile.txt", "../../configfile.txt", "file../.txt", "subdir/file.txt", "file.txt", ".file.txt", "_file-txt_.txt"}
	for i, validName := range validFileNames {
		configRef := createConfig(t, ts, validName, validName)

		serviceSpec = createServiceSpecWithConfigs(fmt.Sprintf("valid%v", i), configRef)
		_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec})
		require.NoError(t, err)
	}

	// test config references with RuntimeTarget
	configRefCredSpec := createConfig(t, ts, "credentialspec", "credentialspec")
	configRefCredSpec.Target = &api.ConfigReference_Runtime{
		Runtime: &api.RuntimeTarget{},
	}
	serviceSpec = createServiceSpecWithConfigs("runtimetarget", configRefCredSpec)
	serviceSpec.Task.GetContainer().Privileges = &api.Privileges{
		CredentialSpec: &api.Privileges_CredentialSpec{
			Source: &api.Privileges_CredentialSpec_Config{
				Config: configRefCredSpec.ConfigID,
			},
		},
	}
	_, err = ts.Client.CreateService(
		context.Background(), &api.CreateServiceRequest{Spec: serviceSpec},
	)
	require.NoError(t, err)

	// test CredentialSpec without ConfigReference
	serviceSpec = createSpec("missingruntimetarget", "imagemissingruntimetarget", 1)
	serviceSpec.Task.GetContainer().Privileges = &api.Privileges{
		CredentialSpec: &api.Privileges_CredentialSpec{
			Source: &api.Privileges_CredentialSpec_Config{
				Config: configRefCredSpec.ConfigID,
			},
		},
	}
	_, err = ts.Client.CreateService(
		context.Background(), &api.CreateServiceRequest{Spec: serviceSpec},
	)
	t.Logf("error when missing configreference: %v", err)
	require.Error(t, err)

	// test config target conflicts on update
	serviceSpec1 := createServiceSpecWithConfigs("service5", configRef2, configRef3)
	// Copy this service, but delete the configs for creation
	serviceSpec2 := serviceSpec1.Copy()
	serviceSpec2.Task.GetContainer().Configs = nil
	rs, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: serviceSpec2})
	require.NoError(t, err)

	// Attempt to update to the originally intended (conflicting) spec
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      rs.Service.ID,
		Spec:           serviceSpec1,
		ServiceVersion: &rs.Service.Meta.Version,
	})
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
}

func TestGetService(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	_, err := ts.Client.GetService(context.Background(), &api.GetServiceRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	_, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: "invalid"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err))

	service := createService(t, ts, "name", "image", 1)
	r, err := ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	require.NoError(t, err)
	service.Meta.Version = r.Service.Meta.Version
	assert.Equal(t, service, r.Service)
}

func TestUpdateService(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	service := createService(t, ts, "name", "image", 1)

	_, err := ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{ServiceID: "invalid", Spec: &service.Spec, ServiceVersion: &api.Version{}})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err))

	// No update options.
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{ServiceID: service.ID, Spec: &service.Spec})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{ServiceID: service.ID, Spec: &service.Spec, ServiceVersion: &service.Meta.Version})
	require.NoError(t, err)

	r, err := ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	require.NoError(t, err)
	assert.Equal(t, service.Spec.Annotations.Name, r.Service.Spec.Annotations.Name)
	mode, ok := r.Service.Spec.GetMode().(*api.ServiceSpec_Replicated)
	assert.True(t, ok)
	assert.Equal(t, 1, mode.Replicated.Replicas)

	mode.Replicated.Replicas = 42
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &r.Service.Spec,
		ServiceVersion: &r.Service.Meta.Version,
	})
	require.NoError(t, err)

	r, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	require.NoError(t, err)
	assert.Equal(t, service.Spec.Annotations.Name, r.Service.Spec.Annotations.Name)
	mode, ok = r.Service.Spec.GetMode().(*api.ServiceSpec_Replicated)
	assert.True(t, ok)
	assert.Equal(t, 42, mode.Replicated.Replicas)

	// mode change not allowed
	r, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	require.NoError(t, err)
	r.Service.Spec.Mode = &api.ServiceSpec_Global{
		Global: &api.GlobalService{},
	}
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &r.Service.Spec,
		ServiceVersion: &r.Service.Meta.Version,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), errModeChangeNotAllowed.Error())

	// Versioning.
	r, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: service.ID})
	require.NoError(t, err)
	version := &r.Service.Meta.Version

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &r.Service.Spec,
		ServiceVersion: version,
	})
	require.NoError(t, err)

	// Perform an update with the "old" version.
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &r.Service.Spec,
		ServiceVersion: version,
	})
	require.Error(t, err)

	// Attempt to update service name; renaming is not implemented
	r.Service.Spec.Annotations.Name = "newname"
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &r.Service.Spec,
		ServiceVersion: version,
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, testutils.ErrorCode(err))

	// test port conflicts
	spec2 := createSpec("name2", "image", 1)
	spec2.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9000), TargetPort: uint32(9000), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec2})
	require.NoError(t, err)

	spec3 := createSpec("name3", "image", 1)
	rs, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec3})
	require.NoError(t, err)

	spec3.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9000), TargetPort: uint32(9000), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      rs.Service.ID,
		Spec:           spec3,
		ServiceVersion: &rs.Service.Meta.Version,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
	spec3.Endpoint = &api.EndpointSpec{Ports: []*api.PortConfig{
		{PublishedPort: uint32(9001), TargetPort: uint32(9000), Protocol: api.PortConfig_Protocol(api.ProtocolTCP)},
	}}
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      rs.Service.ID,
		Spec:           spec3,
		ServiceVersion: &rs.Service.Meta.Version,
	})
	require.NoError(t, err)

	// ingress network cannot be attached explicitly
	spec4 := createSpec("name4", "image", 1)
	rs, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec4})
	require.NoError(t, err)
	spec4.Task.Networks = []*api.NetworkAttachmentConfig{{Target: getIngressTargetID(t, ts)}}
	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      rs.Service.ID,
		Spec:           spec4,
		ServiceVersion: &rs.Service.Meta.Version,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
}

func TestServiceUpdateRejectNetworkChange(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	spec := createSpec("name1", "image", 1)
	spec.Networks = []*api.NetworkAttachmentConfig{
		{
			Target: "net20",
		},
	}
	cr, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)

	ur, err := ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: cr.Service.ID})
	require.NoError(t, err)
	service := ur.Service

	service.Spec.Networks[0].Target = "net30"

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &service.Spec,
		ServiceVersion: &service.Meta.Version,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), errNetworkUpdateNotSupported.Error())

	// Changes to TaskSpec.Networks are allowed
	spec = createSpec("name2", "image", 1)
	spec.Task.Networks = []*api.NetworkAttachmentConfig{
		{
			Target: "net20",
		},
	}
	cr, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)

	ur, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: cr.Service.ID})
	require.NoError(t, err)
	service = ur.Service

	service.Spec.Task.Networks[0].Target = "net30"

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &service.Spec,
		ServiceVersion: &service.Meta.Version,
	})
	require.NoError(t, err)

	// Migrate networks from ServiceSpec.Networks to TaskSpec.Networks
	spec = createSpec("name3", "image", 1)
	spec.Networks = []*api.NetworkAttachmentConfig{
		{
			Target: "net20",
		},
	}
	cr, err = ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)

	ur, err = ts.Client.GetService(context.Background(), &api.GetServiceRequest{ServiceID: cr.Service.ID})
	require.NoError(t, err)
	service = ur.Service

	service.Spec.Task.Networks = spec.Networks
	service.Spec.Networks = nil

	_, err = ts.Client.UpdateService(context.Background(), &api.UpdateServiceRequest{
		ServiceID:      service.ID,
		Spec:           &service.Spec,
		ServiceVersion: &service.Meta.Version,
	})
	require.NoError(t, err)
}

func TestRemoveService(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	_, err := ts.Client.RemoveService(context.Background(), &api.RemoveServiceRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	service := createService(t, ts, "name", "image", 1)
	r, err := ts.Client.RemoveService(context.Background(), &api.RemoveServiceRequest{ServiceID: service.ID})
	require.NoError(t, err)
	assert.NotNil(t, r)
}

func TestValidateEndpointSpec(t *testing.T) {
	endPointSpec1 := &api.EndpointSpec{
		Mode: api.ResolutionModeDNSRoundRobin,
		Ports: []*api.PortConfig{
			{
				Name:       "http",
				TargetPort: 80,
			},
		},
	}

	endPointSpec2 := &api.EndpointSpec{
		Mode: api.ResolutionModeVirtualIP,
		Ports: []*api.PortConfig{
			{
				Name:          "http",
				TargetPort:    81,
				PublishedPort: 8001,
			},
			{
				Name:          "http",
				TargetPort:    80,
				PublishedPort: 8000,
			},
		},
	}

	// has duplicated published port, invalid
	endPointSpec3 := &api.EndpointSpec{
		Mode: api.ResolutionModeVirtualIP,
		Ports: []*api.PortConfig{
			{
				Name:          "http",
				TargetPort:    81,
				PublishedPort: 8001,
			},
			{
				Name:          "http",
				TargetPort:    80,
				PublishedPort: 8001,
			},
		},
	}

	// duplicated published port but different protocols, valid
	endPointSpec4 := &api.EndpointSpec{
		Mode: api.ResolutionModeVirtualIP,
		Ports: []*api.PortConfig{
			{
				Name:          "dns",
				TargetPort:    53,
				PublishedPort: 8002,
				Protocol:      api.ProtocolTCP,
			},
			{
				Name:          "dns",
				TargetPort:    53,
				PublishedPort: 8002,
				Protocol:      api.ProtocolUDP,
			},
		},
	}

	// multiple randomly assigned published ports
	endPointSpec5 := &api.EndpointSpec{
		Mode: api.ResolutionModeVirtualIP,
		Ports: []*api.PortConfig{
			{
				Name:       "http",
				TargetPort: 80,
				Protocol:   api.ProtocolTCP,
			},
			{
				Name:       "dns",
				TargetPort: 53,
				Protocol:   api.ProtocolUDP,
			},
			{
				Name:       "dns",
				TargetPort: 53,
				Protocol:   api.ProtocolTCP,
			},
		},
	}

	err := validateEndpointSpec(endPointSpec1)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	err = validateEndpointSpec(endPointSpec2)
	require.NoError(t, err)

	err = validateEndpointSpec(endPointSpec3)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	err = validateEndpointSpec(endPointSpec4)
	require.NoError(t, err)

	err = validateEndpointSpec(endPointSpec5)
	require.NoError(t, err)
}

func TestServiceEndpointSpecUpdate(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	spec := &api.ServiceSpec{
		Annotations: api.Annotations{
			Name: "name",
		},
		Task: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image: "image",
				},
			},
		},
		Mode: &api.ServiceSpec_Replicated{
			Replicated: &api.ReplicatedService{
				Replicas: 1,
			},
		},
		Endpoint: &api.EndpointSpec{
			Ports: []*api.PortConfig{
				{
					Name:       "http",
					TargetPort: 80,
				},
			},
		},
	}

	r, err := ts.Client.CreateService(context.Background(),
		&api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)
	assert.NotNil(t, r)

	// Update the service with duplicate ports
	spec.Endpoint.Ports = append(spec.Endpoint.Ports, &api.PortConfig{
		Name:       "fakehttp",
		TargetPort: 80,
	})
	_, err = ts.Client.UpdateService(context.Background(),
		&api.UpdateServiceRequest{Spec: spec})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
}

func TestListServices(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	r, err := ts.Client.ListServices(context.Background(), &api.ListServicesRequest{})
	require.NoError(t, err)
	assert.Empty(t, r.Services)

	s1 := createService(t, ts, "name1", "image", 1)
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{})
	require.NoError(t, err)
	assert.Len(t, r.Services, 1)

	createService(t, ts, "name2", "image", 1)
	s3 := createGenericService(t, ts, "name3", "my-runtime")

	// List all.
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{})
	require.NoError(t, err)
	assert.Len(t, r.Services, 3)

	// List by runtime.
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			Runtimes: []string{"container"},
		},
	})
	require.NoError(t, err)
	assert.Len(t, r.Services, 2)

	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			Runtimes: []string{"my-runtime"},
		},
	})
	require.NoError(t, err)
	assert.Len(t, r.Services, 1)
	assert.Equal(t, s3.ID, r.Services[0].ID)

	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			Runtimes: []string{"invalid"},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, r.Services)

	// List with an ID prefix.
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			IDPrefixes: []string{s1.ID[0:4]},
		},
	})
	require.NoError(t, err)
	assert.Len(t, r.Services, 1)
	assert.Equal(t, s1.ID, r.Services[0].ID)

	// List with simple filter.
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			NamePrefixes: []string{"name1"},
		},
	})
	require.NoError(t, err)
	assert.Len(t, r.Services, 1)

	// List with union filter.
	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			NamePrefixes: []string{"name1", "name2"},
		},
	})
	require.NoError(t, err)
	assert.Len(t, r.Services, 2)

	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			NamePrefixes: []string{"name1", "name2", "name4"},
		},
	})
	require.NoError(t, err)
	assert.Len(t, r.Services, 2)

	r, err = ts.Client.ListServices(context.Background(), &api.ListServicesRequest{
		Filters: &api.ListServicesRequest_Filters{
			NamePrefixes: []string{"name4"},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, r.Services)

	// List with filter intersection.
	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				NamePrefixes: []string{"name1"},
				IDPrefixes:   []string{s1.ID},
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, r.Services, 1)

	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				NamePrefixes: []string{"name2"},
				IDPrefixes:   []string{s1.ID},
			},
		},
	)
	require.NoError(t, err)
	assert.Empty(t, r.Services)

	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				NamePrefixes: []string{"name3"},
				Runtimes:     []string{"my-runtime"},
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, r.Services, 1)

	// List filter by label.
	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				Labels: map[string]string{
					"common": "yes",
				},
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, r.Services, 3)

	// Value-less label.
	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				Labels: map[string]string{
					"common": "",
				},
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, r.Services, 3)

	// Label intersection.
	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				Labels: map[string]string{
					"common": "",
					"unique": "name1",
				},
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, r.Services, 1)

	r, err = ts.Client.ListServices(context.Background(),
		&api.ListServicesRequest{
			Filters: &api.ListServicesRequest_Filters{
				Labels: map[string]string{
					"common": "",
					"unique": "error",
				},
			},
		},
	)
	require.NoError(t, err)
	assert.Empty(t, r.Services)
}

func TestListServiceStatuses(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	// Test listing no services is empty and has no error
	r, err := ts.Client.ListServiceStatuses(
		context.Background(),
		&api.ListServiceStatusesRequest{},
	)
	require.NoError(t, err, "error when listing no services against an empty store")
	assert.NotNil(t, r, "response against an empty store was nil")
	assert.Empty(t, r.Statuses, "response statuses was not empty")

	// Test listing services that do not exist. We should still get a response,
	// but both desired and actual should be 0
	r, err = ts.Client.ListServiceStatuses(
		context.Background(),
		&api.ListServiceStatusesRequest{Services: []string{"foo"}},
	)
	require.NoError(t, err, "error listing services that do not exist")
	assert.NotNil(t, r, "response for nonexistant services was nil")
	assert.Len(t, r.Statuses, 1, "expected 1 status")
	assert.Equal(
		t, &api.ListServiceStatusesResponse_ServiceStatus{ServiceID: "foo"}, r.Statuses[0],
	)

	// now test that listing service statuses actually works.

	// justRight will be converged
	justRight := createService(t, ts, "justRight", "image", 3)
	// notEnough will not have enough tasks in running
	notEnough := createService(t, ts, "notEnough", "image", 7)

	// no shortcut for creating a global service
	globalSpec := createSpec("global", "image", 0)
	globalSpec.Mode = &api.ServiceSpec_Global{Global: &api.GlobalService{}}

	svcResp, svcErr := ts.Client.CreateService(
		context.Background(), &api.CreateServiceRequest{Spec: globalSpec},
	)
	require.NoError(t, svcErr)

	// global will have the right number of tasks
	global := svcResp.Service

	global2Spec := globalSpec.Copy()
	global2Spec.Annotations.Name = "global2"

	svcResp, svcErr = ts.Client.CreateService(
		context.Background(), &api.CreateServiceRequest{Spec: global2Spec},
	)
	require.NoError(t, svcErr)

	// global2 will not have enough tasks
	global2 := svcResp.Service

	// over will have too many tasks running (as would be seen in a scale-down
	over := createService(t, ts, "over", "image", 2)

	// replicatedJob1 will be partly completed
	replicatedJob1Spec := createSpec("replicatedJob1", "image", 0)
	replicatedJob1Spec.Mode = &api.ServiceSpec_ReplicatedJob{
		ReplicatedJob: &api.ReplicatedJob{
			MaxConcurrent:    2,
			TotalCompletions: 10,
		},
	}

	svcResp, svcErr = ts.Client.CreateService(
		context.Background(), &api.CreateServiceRequest{Spec: replicatedJob1Spec},
	)
	require.NoError(t, svcErr)
	assert.NotNil(t, svcResp)
	replicatedJob1 := svcResp.Service

	// replicatedJob2 has been executed before, and will have tasks from a
	// previous JobIteration
	replicatedJob2Spec := replicatedJob1Spec.Copy()
	replicatedJob2Spec.Annotations.Name = "replicatedJob2"
	svcResp, svcErr = ts.Client.CreateService(
		context.Background(), &api.CreateServiceRequest{Spec: replicatedJob2Spec},
	)
	require.NoError(t, svcErr)
	assert.NotNil(t, svcResp)
	replicatedJob2 := svcResp.Service

	// globalJob is a partly complete global job
	globalJobSpec := createSpec("globalJob", "image", 0)
	globalJobSpec.Mode = &api.ServiceSpec_GlobalJob{
		GlobalJob: &api.GlobalJob{},
	}
	svcResp, svcErr = ts.Client.CreateService(
		context.Background(), &api.CreateServiceRequest{Spec: globalJobSpec},
	)
	require.NoError(t, svcErr)
	assert.NotNil(t, svcResp)
	globalJob := svcResp.Service

	// now create some tasks. use a quick helper function for this
	createTask := func(s *api.Service, actual api.TaskState, desired api.TaskState, opts ...func(*api.Service, *api.Task)) *api.Task {
		task := &api.Task{
			ID:           identity.NewID(),
			DesiredState: desired,
			Spec:         s.Spec.Task,
			Status: api.TaskStatus{
				State: actual,
			},
			ServiceID: s.ID,
		}

		for _, opt := range opts {
			opt(s, task)
		}

		err := ts.Store.Update(func(tx store.Tx) error {
			return store.CreateTask(tx, task)
		})
		require.NoError(t, err)
		return task
	}

	withJobIteration := func(s *api.Service, task *api.Task) {
		assert.NotNil(t, s.JobStatus)
		task.JobIteration = &(s.JobStatus.JobIteration)
	}

	// alias task states for brevity
	running := api.TaskStateRunning
	shutdown := api.TaskStateShutdown
	completed := api.TaskStateCompleted
	newt := api.TaskStateNew
	failed := api.TaskStateFailed

	// create 3 running tasks for justRight
	for i := 0; i < 3; i++ {
		createTask(justRight, running, running)
	}
	// create 2 failed and 2 shutdown tasks
	for i := 0; i < 2; i++ {
		createTask(justRight, failed, shutdown)
		createTask(justRight, shutdown, shutdown)
	}

	// create 4 tasks for notEnough
	for i := 0; i < 4; i++ {
		createTask(notEnough, running, running)
	}
	// create 3 tasks in new state
	for i := 0; i < 3; i++ {
		createTask(notEnough, newt, running)
	}
	// create 1 failed and 1 shutdown task
	createTask(notEnough, failed, shutdown)
	createTask(notEnough, shutdown, shutdown)

	// create 2 tasks out of 2 desired for global
	for i := 0; i < 2; i++ {
		createTask(global, running, running)
	}
	// create 3 shutdown tasks for global
	for i := 0; i < 3; i++ {
		createTask(global, shutdown, shutdown)
	}

	// create 4 out of 5 tasks for global2
	for i := 0; i < 4; i++ {
		createTask(global2, running, running)
	}
	createTask(global2, newt, running)

	// create 6 failed tasks
	for i := 0; i < 6; i++ {
		createTask(global2, failed, shutdown)
	}

	// create 4 out of 2 tasks. no shutdown or failed tasks.  this would be the
	// case if you did a call immediately after updating the service, before
	// the orchestrator had updated the task desired states
	for i := 0; i < 4; i++ {
		createTask(over, running, running)
	}

	// create 2 running tasks for replicatedJob1
	for i := 0; i < 2; i++ {
		createTask(replicatedJob1, running, completed, withJobIteration)
	}

	// create 4 completed tasks for replicatedJob1
	for i := 0; i < 4; i++ {
		createTask(replicatedJob1, completed, completed, withJobIteration)
	}

	// create 10 completed tasks for replicatedJob2
	for i := 0; i < 10; i++ {
		createTask(replicatedJob2, completed, completed, withJobIteration)
	}

	replicatedJob2Spec.Task.ForceUpdate++

	// now update replicatedJob2, so JobIteration gets incremented
	updateResp, updateErr := ts.Client.UpdateService(
		context.Background(),
		&api.UpdateServiceRequest{
			ServiceID:      replicatedJob2.ID,
			ServiceVersion: &replicatedJob2.Meta.Version,
			Spec:           replicatedJob2Spec,
		},
	)
	require.NoError(t, updateErr)
	assert.NotNil(t, updateResp)
	replicatedJob2 = updateResp.Service

	// and create 1 tasks out of 2
	createTask(replicatedJob2, running, completed, withJobIteration)
	// and 3 completed already
	for i := 0; i < 3; i++ {
		createTask(replicatedJob2, completed, completed, withJobIteration)
	}

	// create 5 running tasks for globalJob
	for i := 0; i < 5; i++ {
		createTask(globalJob, running, completed, withJobIteration)
	}
	// create 3 completed tasks
	for i := 0; i < 3; i++ {
		createTask(globalJob, completed, completed, withJobIteration)
	}

	// now, create a service that has already been deleted, but has dangling
	// tasks
	goneSpec := createSpec("gone", "image", 3)
	gone := &api.Service{
		ID:   identity.NewID(),
		Spec: *goneSpec,
	}

	for i := 0; i < 3; i++ {
		createTask(gone, running, shutdown)
		createTask(gone, shutdown, shutdown)
	}

	// now list service statuses
	r, err = ts.Client.ListServiceStatuses(
		context.Background(),
		&api.ListServiceStatusesRequest{Services: []string{
			justRight.ID, notEnough.ID, global.ID, global2.ID,
			replicatedJob1.ID, replicatedJob2.ID, globalJob.ID, over.ID, gone.ID,
		}},
	)
	require.NoError(t, err, "error getting service statuses")
	assert.NotNil(t, r, "service status response is nil")
	assert.Len(t, r.Statuses, 9)

	expected := map[string]*api.ListServiceStatusesResponse_ServiceStatus{
		"justRight": {
			ServiceID:    justRight.ID,
			DesiredTasks: 3,
			RunningTasks: 3,
		},
		"notEnough": {
			ServiceID:    notEnough.ID,
			DesiredTasks: 7,
			RunningTasks: 4,
		},
		"global": {
			ServiceID:    global.ID,
			DesiredTasks: 2,
			RunningTasks: 2,
		},
		"global2": {
			ServiceID:    global2.ID,
			DesiredTasks: 5,
			RunningTasks: 4,
		},
		"over": {
			ServiceID:    over.ID,
			DesiredTasks: 2,
			RunningTasks: 4,
		},
		"replicatedJob1": {
			ServiceID:      replicatedJob1.ID,
			DesiredTasks:   2,
			RunningTasks:   2,
			CompletedTasks: 4,
		},
		"replicatedJob2": {
			ServiceID:      replicatedJob2.ID,
			DesiredTasks:   2,
			RunningTasks:   1,
			CompletedTasks: 3,
		},
		"globalJob": {
			ServiceID:      globalJob.ID,
			DesiredTasks:   5,
			RunningTasks:   5,
			CompletedTasks: 3,
		},
		"gone": {
			ServiceID:    gone.ID,
			DesiredTasks: 0,
			RunningTasks: 3,
		},
	}

	// compare expected and actual values. make sure all are used by keeping
	// track of which we visited. i borrowed this pattern from
	// assert.ElementsMatch, which is in a newer version of that library
	visited := make([]bool, len(expected))
	for name, expect := range expected {
		found := false
		for i := 0; i < len(r.Statuses); i++ {
			if visited[i] {
				continue
			}
			if reflect.DeepEqual(expect, r.Statuses[i]) {
				visited[i] = true
				found = true
				break
			}
		}
		assert.Truef(t, found, "did not find status for %v in response", name)
	}
}

// TestJobService tests that if a job-mode service is created, the necessary
// fields are all initialized to the correct values. Then, it tests that if a
// job-mode service is updated, the fields are updated to correct values.
func TestJobService(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	// first, create a replicated job mode service spec
	spec := &api.ServiceSpec{
		Annotations: api.Annotations{
			Name: "replicatedjob",
		},
		Task: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image: "image",
				},
			},
		},
		Mode: &api.ServiceSpec_ReplicatedJob{
			ReplicatedJob: &api.ReplicatedJob{
				MaxConcurrent:    3,
				TotalCompletions: 9,
			},
		},
	}

	before := gogotypes.TimestampNow()
	// now, create the service
	resp, err := ts.Client.CreateService(
		context.Background(), &api.CreateServiceRequest{
			Spec: spec,
		},
	)
	after := gogotypes.TimestampNow()

	// ensure there are no errors
	require.NoError(t, err)
	// and assert that the response is valid
	require.NotNil(t, resp)
	require.NotNil(t, resp.Service)
	// ensure that the service has a JobStatus set
	require.NotNil(t, resp.Service.JobStatus, "expected JobStatus to not be nil")
	// and ensure that JobStatus.JobIteration is set to 0, which is the default
	require.Equal(
		t, uint64(0), resp.Service.JobStatus.JobIteration.Index,
		"expected JobIteration for new replicated job to be 0",
	)
	require.NotNil(t, resp.Service.JobStatus.LastExecution)
	assert.GreaterOrEqual(t, resp.Service.JobStatus.LastExecution.Compare(before), 0,
		"expected %v to be after %v", resp.Service.JobStatus.LastExecution, before,
	)
	assert.LessOrEqual(t, resp.Service.JobStatus.LastExecution.Compare(after), 0,
		"expected %v to be before %v", resp.Service.JobStatus.LastExecution, after,
	)

	// now, repeat all of the above, but with a global job
	gspec := spec.Copy()
	gspec.Annotations.Name = "globaljob"
	gspec.Mode = &api.ServiceSpec_GlobalJob{
		GlobalJob: &api.GlobalJob{},
	}
	before = gogotypes.TimestampNow()
	gresp, gerr := ts.Client.CreateService(
		context.Background(), &api.CreateServiceRequest{
			Spec: gspec,
		},
	)
	after = gogotypes.TimestampNow()

	require.NoError(t, gerr)
	require.NotNil(t, gresp)
	require.NotNil(t, gresp.Service)
	require.NotNil(t, gresp.Service.JobStatus)
	require.Equal(
		t, uint64(0), gresp.Service.JobStatus.JobIteration.Index,
		"expected JobIteration for new global job to be 0",
	)
	require.NotNil(t, gresp.Service.JobStatus.LastExecution)
	assert.GreaterOrEqual(t, gresp.Service.JobStatus.LastExecution.Compare(before), 0,
		"expected %v to be after %v", gresp.Service.JobStatus.LastExecution, before,
	)
	assert.LessOrEqual(t, gresp.Service.JobStatus.LastExecution.Compare(after), 0,
		"expected %v to be before %v", gresp.Service.JobStatus.LastExecution, after,
	)

	// now test that updating the service increments the JobIteration
	spec.Task.ForceUpdate = spec.Task.ForceUpdate + 1
	before = gogotypes.TimestampNow()
	uresp, uerr := ts.Client.UpdateService(
		context.Background(), &api.UpdateServiceRequest{
			ServiceID:      resp.Service.ID,
			ServiceVersion: &(resp.Service.Meta.Version),
			Spec:           spec,
		},
	)
	after = gogotypes.TimestampNow()

	require.NoError(t, uerr)
	require.NotNil(t, uresp)
	require.NotNil(t, uresp.Service)
	require.NotNil(t, uresp.Service.JobStatus)
	// updating the service should bump the JobStatus.JobIteration.Index by 1
	require.Equal(
		t, uint64(1), uresp.Service.JobStatus.JobIteration.Index,
		"expected JobIteration for updated replicated job to be 1",
	)
	require.NotNil(t, uresp.Service.JobStatus.LastExecution)
	assert.GreaterOrEqual(t, uresp.Service.JobStatus.LastExecution.Compare(before), 0,
		"expected %v to be after %v", uresp.Service.JobStatus.LastExecution, before,
	)
	assert.LessOrEqual(t, uresp.Service.JobStatus.LastExecution.Compare(after), 0,
		"expected %v to be before %v", uresp.Service.JobStatus.LastExecution, after,
	)

	// rinse and repeat
	gspec.Task.ForceUpdate = spec.Task.ForceUpdate + 1
	before = gogotypes.TimestampNow()
	guresp, guerr := ts.Client.UpdateService(
		context.Background(), &api.UpdateServiceRequest{
			ServiceID:      gresp.Service.ID,
			ServiceVersion: &(gresp.Service.Meta.Version),
			Spec:           gspec,
		},
	)
	after = gogotypes.TimestampNow()

	require.NoError(t, guerr)
	require.NotNil(t, guresp)
	require.NotNil(t, guresp.Service)
	require.NotNil(t, guresp.Service.JobStatus)
	require.Equal(
		t, uint64(1), guresp.Service.JobStatus.JobIteration.Index,
		"expected JobIteration for updated replicated job to be 1",
	)
	require.NotNil(t, guresp.Service.JobStatus.LastExecution)
	assert.GreaterOrEqual(t, guresp.Service.JobStatus.LastExecution.Compare(before), 0,
		"expected %v to be after %v", guresp.Service.JobStatus.LastExecution, before,
	)
	assert.LessOrEqual(t, guresp.Service.JobStatus.LastExecution.Compare(after), 0,
		"expected %v to be before %v", guresp.Service.JobStatus.LastExecution, after,
	)
}

// TestServiceValidateJob tests that calling the service API correctly
// validates a job-mode service. Some fields, like UpdateConfig, are not valid
// for job-mode services, and should return an error if they're set
func TestServiceValidateJob(t *testing.T) {
	bad := &api.ServiceSpec{
		Mode:   &api.ServiceSpec_ReplicatedJob{ReplicatedJob: &api.ReplicatedJob{}},
		Update: &api.UpdateConfig{},
	}

	err := validateJob(bad)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	good := []*api.ServiceSpec{
		{
			Mode: &api.ServiceSpec_ReplicatedJob{
				ReplicatedJob: &api.ReplicatedJob{
					MaxConcurrent: 3, TotalCompletions: 9,
				},
			},
		},
		{Mode: &api.ServiceSpec_GlobalJob{}},
	}

	for _, g := range good {
		err := validateJob(g)
		require.NoError(t, err)
	}
}
