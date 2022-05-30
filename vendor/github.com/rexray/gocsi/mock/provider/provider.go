package provider

import (
	"context"
	"net"

	log "github.com/sirupsen/logrus"

	"github.com/rexray/gocsi"
	"github.com/rexray/gocsi/mock/service"
)

// New returns a new Mock Storage Plug-in Provider.
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
			// Enable serial volume access.
			gocsi.EnvVarSerialVolAccess + "=true",

			// Enable request and response validation.
			gocsi.EnvVarSpecValidation + "=true",

			// Treat the following fields as required:
			//   * ControllerPublishVolumeResponse.PublishContext
			//   * NodeStageVolumeRequest.PublishContext
			//   * NodePublishVolumeRequest.PublishContext
			gocsi.EnvVarRequirePubContext + "=true",
		},
	}
}
