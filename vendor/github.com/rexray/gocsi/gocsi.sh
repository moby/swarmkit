#!/bin/sh

HOME=${HOME:-/tmp}
GOPATH=${GOPATH:-$HOME/go}
GOPATH=$(echo "$GOPATH" | awk '{print $1}')

if [ "$1" = "" ]; then
  echo "usage: $0 GO_IMPORT_PATH"
  exit 1
fi

SP_PATH=$1
SP_DIR=$GOPATH/src/$SP_PATH
SP_NAME=$(basename "$SP_PATH")

echo "creating project directories:"
echo "  $SP_DIR"
echo "  $SP_DIR/provider"
echo "  $SP_DIR/service"
mkdir -p "$SP_DIR" "$SP_DIR/provider" "$SP_DIR/service"
cd "$SP_DIR" > /dev/null 2>&1 || exit 1

echo "creating project files:"
echo "  $SP_DIR/main.go"
cat << EOF > "main.go"
package main

import (
	"context"

	"github.com/rexray/gocsi"

	"$SP_PATH/provider"
	"$SP_PATH/service"
)

// main is ignored when this package is built as a go plug-in.
func main() {
	gocsi.Run(
		context.Background(),
		service.Name,
		"A description of the SP",
		"",
		provider.New())
}
EOF

echo "  $SP_DIR/provider/provider.go"
cat << EOF > "provider/provider.go"
package provider

import (
	"context"
	"net"

	log "github.com/sirupsen/logrus"
	"github.com/rexray/gocsi"

	"$SP_PATH/service"
)

// New returns a new CSI Storage Plug-in Provider.
func New() gocsi.StoragePluginProvider {
	svc := service.New()
	return &gocsi.StoragePlugin{
		Controller: svc,
		Identity:   svc,
		Node:       svc,

		// BeforeServe allows the SP to participate in the startup
		// sequence. This function is invoked directly before the
		// gRPC server is created, giving the callback the ability to
		// modify the SP's interceptors, server options, or prevent the
		// server from starting by returning a non-nil error.
		BeforeServe: func(
			ctx context.Context,
			sp *gocsi.StoragePlugin,
			lis net.Listener) error {

			log.WithField("service", service.Name).Debug("BeforeServe")
			return nil
		},

		EnvVars: []string{
			// Enable request validation.
			gocsi.EnvVarSpecReqValidation + "=true",

			// Enable serial volume access.
			gocsi.EnvVarSerialVolAccess + "=true",
		},
	}
}
EOF

echo "  $SP_DIR/service/service.go"
cat << EOF > "service/service.go"
package service

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
)

const (
	// Name is the name of this CSI SP.
	Name = "$SP_NAME"

	// VendorVersion is the version of this CSP SP.
	VendorVersion = "0.0.0"
)

// Service is a CSI SP and idempotency.Provider.
type Service interface {
	csi.ControllerServer
	csi.IdentityServer
	csi.NodeServer
}

type service struct{}

// New returns a new Service.
func New() Service {
	return &service{}
}
EOF

echo "  $SP_DIR/service/controller.go"
cat << EOF > "service/controller.go"
package service

import (
	"golang.org/x/net/context"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

func (s *service) CreateVolume(
	ctx context.Context,
	req *csi.CreateVolumeRequest) (
	*csi.CreateVolumeResponse, error) {

	return nil, nil
}

func (s *service) DeleteVolume(
	ctx context.Context,
	req *csi.DeleteVolumeRequest) (
	*csi.DeleteVolumeResponse, error) {

	return nil, nil
}

func (s *service) ControllerPublishVolume(
	ctx context.Context,
	req *csi.ControllerPublishVolumeRequest) (
	*csi.ControllerPublishVolumeResponse, error) {

	return nil, nil
}

func (s *service) ControllerUnpublishVolume(
	ctx context.Context,
	req *csi.ControllerUnpublishVolumeRequest) (
	*csi.ControllerUnpublishVolumeResponse, error) {

	return nil, nil
}

func (s *service) ValidateVolumeCapabilities(
	ctx context.Context,
	req *csi.ValidateVolumeCapabilitiesRequest) (
	*csi.ValidateVolumeCapabilitiesResponse, error) {

	return nil, nil
}

func (s *service) ListVolumes(
	ctx context.Context,
	req *csi.ListVolumesRequest) (
	*csi.ListVolumesResponse, error) {

	return nil, nil
}

func (s *service) GetCapacity(
	ctx context.Context,
	req *csi.GetCapacityRequest) (
	*csi.GetCapacityResponse, error) {

	return nil, nil
}

func (s *service) ControllerGetCapabilities(
	ctx context.Context,
	req *csi.ControllerGetCapabilitiesRequest) (
	*csi.ControllerGetCapabilitiesResponse, error) {

	return nil, nil
}

func (s *service) CreateSnapshot(
	ctx context.Context,
	req *csi.CreateSnapshotRequest) (
	*csi.CreateSnapshotResponse, error) {

	return nil, nil
}

func (s *service) DeleteSnapshot(
	ctx context.Context,
	req *csi.DeleteSnapshotRequest) (
	*csi.DeleteSnapshotResponse, error) {

	return nil, nil
}

func (s *service) ListSnapshots(
	ctx context.Context,
	req *csi.ListSnapshotsRequest) (
	*csi.ListSnapshotsResponse, error) {

	return nil, nil
}

func (s *service) ControllerExpandVolume(
	ctx context.Context,
	req *csi.ControllerExpandVolumeRequest) (
	*csi.ControllerExpandVolumeResponse, error) {

	return nil, nil
}

EOF

echo "  $SP_DIR/service/identity.go"
cat << EOF > "service/identity.go"
package service

import (
	"golang.org/x/net/context"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

func (s *service) GetPluginInfo(
	ctx context.Context,
	req *csi.GetPluginInfoRequest) (
	*csi.GetPluginInfoResponse, error) {

	return nil, nil
}

func (s *service) GetPluginCapabilities(
	ctx context.Context,
	req *csi.GetPluginCapabilitiesRequest) (
	*csi.GetPluginCapabilitiesResponse, error) {

	return nil, nil
}

func (s *service) Probe(
	ctx context.Context,
	req *csi.ProbeRequest) (
	*csi.ProbeResponse, error) {

	return nil, nil
}
EOF

echo "  $SP_DIR/service/node.go"
cat << EOF > "service/node.go"
package service

import (
	"golang.org/x/net/context"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

func (s *service) NodeStageVolume(
	ctx context.Context,
	req *csi.NodeStageVolumeRequest) (
	*csi.NodeStageVolumeResponse, error) {

	return nil, nil
}

func (s *service) NodeUnstageVolume(
	ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error) {

	return nil, nil
}

func (s *service) NodePublishVolume(
	ctx context.Context,
	req *csi.NodePublishVolumeRequest) (
	*csi.NodePublishVolumeResponse, error) {

	return nil, nil
}

func (s *service) NodeUnpublishVolume(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest) (
	*csi.NodeUnpublishVolumeResponse, error) {

	return nil, nil
}

func (s *service) NodeGetVolumeStats(
	ctx context.Context,
	req *csi.NodeGetVolumeStatsRequest) (
	*csi.NodeGetVolumeStatsResponse, error) {

	return nil, nil
}

func (s *service) NodeExpandVolume(
	ctx context.Context,
	req *csi.NodeExpandVolumeRequest) (
	*csi.NodeExpandVolumeResponse, error) {

	return nil, nil
}

func (s *service) NodeGetCapabilities(
	ctx context.Context,
	req *csi.NodeGetCapabilitiesRequest) (
	*csi.NodeGetCapabilitiesResponse, error) {

	return nil, nil
}

func (s *service) NodeGetInfo(
	ctx context.Context,
	req *csi.NodeGetInfoRequest) (
	*csi.NodeGetInfoResponse, error) {

	return nil, nil
}
EOF


echo "building $SP_NAME:"
go mod download && go mod verify
go build .
BUILD_RESULT=$?

cd - > /dev/null 2>&1 || exit 1

if [ "$BUILD_RESULT" -eq 0 ]; then
  echo "  success!"
  echo '  example: CSI_ENDPOINT=csi.sock \'
  echo '           X_CSI_LOG_LEVEL=info \'
  echo "           $SP_DIR/$SP_NAME"
  echo
  echo "  help available online at"
  echo "  https://github.com/rexray/gocsi#bootstrapping-a-storage-plug-in"
else
  exit 1
fi
