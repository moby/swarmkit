package defaults

import (
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/deepcopy"
	gogotypes "github.com/gogo/protobuf/types"
)

// Service is a ServiceSpec object with all fields filled in using default
// values.
var Service = api.ServiceSpec{
	Task: api.TaskSpec{
		Runtime: &api.TaskSpec_Container{
			Container: &api.ContainerSpec{
				StopGracePeriod: gogotypes.DurationProto(10 * time.Second),
				PullOptions:     &api.ContainerSpec_PullOptions{},
				DNSConfig:       &api.ContainerSpec_DNSConfig{},
			},
		},
		Resources: &api.ResourceRequirements{},
		Restart: &api.RestartPolicy{
			Condition: api.RestartOnAny,
			Delay:     gogotypes.DurationProto(5 * time.Second),
			Backoff: &api.BackoffPolicy{
				Base:   gogotypes.DurationProto(0 * time.Second),
				Factor: gogotypes.DurationProto(5 * time.Second),
				Max:    gogotypes.DurationProto(30 * time.Minute),
			},
		},
		Placement: &api.Placement{},
	},
	Update: &api.UpdateConfig{
		FailureAction: api.UpdateConfig_PAUSE,
		Monitor:       gogotypes.DurationProto(5 * time.Second),
		Parallelism:   1,
		Order:         api.UpdateConfig_STOP_FIRST,
	},
	Rollback: &api.UpdateConfig{
		FailureAction: api.UpdateConfig_PAUSE,
		Monitor:       gogotypes.DurationProto(5 * time.Second),
		Parallelism:   1,
		Order:         api.UpdateConfig_STOP_FIRST,
	},
}

// InterpolateService returns a ServiceSpec based on the provided spec, which
// has all unspecified values filled in with default values.
func InterpolateService(origSpec *api.ServiceSpec) *api.ServiceSpec {
	spec := origSpec.Copy()

	container := spec.Task.GetContainer()
	defaultContainer := Service.Task.GetContainer()
	if container != nil {
		if container.StopGracePeriod == nil {
			container.StopGracePeriod = &gogotypes.Duration{}
			deepcopy.Copy(container.StopGracePeriod, defaultContainer.StopGracePeriod)
		}
		if container.PullOptions == nil {
			container.PullOptions = defaultContainer.PullOptions.Copy()
		}
		if container.DNSConfig == nil {
			container.DNSConfig = defaultContainer.DNSConfig.Copy()
		}
	}

	if spec.Task.Resources == nil {
		spec.Task.Resources = Service.Task.Resources.Copy()
	}

	if spec.Task.Restart == nil {
		spec.Task.Restart = Service.Task.Restart.Copy()
	} else {
		if spec.Task.Restart.Delay == nil {
			spec.Task.Restart.Delay = &gogotypes.Duration{}
			deepcopy.Copy(spec.Task.Restart.Delay, Service.Task.Restart.Delay)
		}
		if spec.Task.Backoff == nil {
			spec.Task.Restart.Backoff = Service.Task.Restart.Backoff.Copy()
		} else {
			if spec.Task.Restart.Backoff.Base == nil {
				spec.Task.Restart.Backoff.Base = &gogotypes.Duration{}
				deepcopy.Copy(spec.Task.Restart.Backoff.Base, Service.Task.Restart.Backoff.Base)
			}
			if spec.Task.Restart.Backoff.Factor == nil {
				spec.Task.Restart.Backoff.Factor = &gogotypes.Duration{}
				deepcopy.Copy(spec.Task.Restart.Backoff.Factor, Service.Task.Restart.Backoff.Factor)
			}
			if spec.Task.Restart.Backoff.Max == nil {
				spec.Task.Restart.Backoff.Max = &gogotypes.Duration{}
				deepcopy.Copy(spec.Task.Restart.Backoff.Max, Service.Task.Restart.Backoff.Max)
			}
		}
	}

	if spec.Task.Placement == nil {
		spec.Task.Placement = Service.Task.Placement.Copy()
	}

	if spec.Update == nil {
		spec.Update = Service.Update.Copy()
	} else {
		if spec.Update.Monitor == nil {
			spec.Update.Monitor = &gogotypes.Duration{}
			deepcopy.Copy(spec.Update.Monitor, Service.Update.Monitor)
		}
	}

	if spec.Rollback == nil {
		spec.Rollback = Service.Rollback.Copy()
	} else {
		if spec.Rollback.Monitor == nil {
			spec.Rollback.Monitor = &gogotypes.Duration{}
			deepcopy.Copy(spec.Rollback.Monitor, Service.Rollback.Monitor)
		}
	}

	return spec
}
