package cnm

import (
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func TestValidateDriver(t *testing.T) {
	nm := New(nil)
	assert.NoError(t, nm.(*cnm).validateDriver(nil, ""))

	err := nm.(*cnm).validateDriver(&api.Driver{Name: ""}, "")
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
}

func TestValidateIPAMConfiguration(t *testing.T) {
	err := validateIPAMConfiguration(nil)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf := &api.IPAMConfig{
		Subnet: "",
	}

	err = validateIPAMConfiguration(IPAMConf)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf.Subnet = "bad"
	err = validateIPAMConfiguration(IPAMConf)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf.Subnet = "192.168.0.0/16"
	err = validateIPAMConfiguration(IPAMConf)
	assert.NoError(t, err)

	IPAMConf.Range = "bad"
	err = validateIPAMConfiguration(IPAMConf)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf.Range = "192.169.1.0/24"
	err = validateIPAMConfiguration(IPAMConf)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf.Range = "192.168.1.0/24"
	err = validateIPAMConfiguration(IPAMConf)
	assert.NoError(t, err)

	IPAMConf.Gateway = "bad"
	err = validateIPAMConfiguration(IPAMConf)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf.Gateway = "192.169.1.1"
	err = validateIPAMConfiguration(IPAMConf)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf.Gateway = "192.168.1.1"
	err = validateIPAMConfiguration(IPAMConf)
	assert.NoError(t, err)
}
