package controlapi

import (
	"strings"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func buildFilters(by func(string) store.By, values []string) store.By {
	filters := make([]store.By, 0, len(values))
	for _, v := range values {
		filters = append(filters, by(v))
	}
	return store.Or(filters...)
}

func filterContains(match string, candidates []string) bool {
	if len(candidates) == 0 {
		return true
	}
	for _, c := range candidates {
		if c == match {
			return true
		}
	}
	return false
}

func filterContainsPrefix(match string, candidates []string) bool {
	if len(candidates) == 0 {
		return true
	}
	for _, c := range candidates {
		if strings.HasPrefix(c, match) {
			return true
		}
	}
	return false
}

func filterMatchLabels(match map[string]string, candidates map[string]string) bool {
	if len(candidates) == 0 {
		return true
	}

	for k, v := range candidates {
		c, ok := match[k]
		if !ok {
			return false
		}
		if v != "" && v != c {
			return false
		}
	}
	return true
}

func validateAnnotations(m api.Annotations) error {
	if m.Name == "" {
		return grpc.Errorf(codes.InvalidArgument, "meta: name must be provided")
	}
	return nil
}

func validateDriver(driver *api.Driver) error {
	if driver == nil {
		// It is ok to not specify the driver. We will choose
		// a default driver.
		return nil
	}

	if driver.Name == "" {
		return grpc.Errorf(codes.InvalidArgument, "driver name: if driver is specified name is required")
	}

	return nil
}
