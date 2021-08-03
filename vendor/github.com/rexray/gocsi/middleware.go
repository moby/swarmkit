package gocsi

import (
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	csictx "github.com/rexray/gocsi/context"
	"github.com/rexray/gocsi/middleware/logging"
	"github.com/rexray/gocsi/middleware/requestid"
	"github.com/rexray/gocsi/middleware/serialvolume"
	"github.com/rexray/gocsi/middleware/serialvolume/etcd"
	"github.com/rexray/gocsi/middleware/specvalidator"
	"github.com/rexray/gocsi/utils"
)

func (sp *StoragePlugin) initInterceptors(ctx context.Context) {

	sp.Interceptors = append(sp.Interceptors, sp.injectContext)
	log.Debug("enabled context injector")

	var (
		withReqLogging         = sp.getEnvBool(ctx, EnvVarReqLogging)
		withRepLogging         = sp.getEnvBool(ctx, EnvVarRepLogging)
		withDisableLogVolCtx   = sp.getEnvBool(ctx, EnvVarLoggingDisableVolCtx)
		withSerialVol          = sp.getEnvBool(ctx, EnvVarSerialVolAccess)
		withSpec               = sp.getEnvBool(ctx, EnvVarSpecValidation)
		withStgTgtPath         = sp.getEnvBool(ctx, EnvVarRequireStagingTargetPath)
		withVolContext         = sp.getEnvBool(ctx, EnvVarRequireVolContext)
		withPubContext         = sp.getEnvBool(ctx, EnvVarRequirePubContext)
		withCreds              = sp.getEnvBool(ctx, EnvVarCreds)
		withCredsNewVol        = sp.getEnvBool(ctx, EnvVarCredsCreateVol)
		withCredsDelVol        = sp.getEnvBool(ctx, EnvVarCredsDeleteVol)
		withCredsCtrlrPubVol   = sp.getEnvBool(ctx, EnvVarCredsCtrlrPubVol)
		withCredsCtrlrUnpubVol = sp.getEnvBool(ctx, EnvVarCredsCtrlrUnpubVol)
		withCredsNodeStgVol    = sp.getEnvBool(ctx, EnvVarCredsNodeStgVol)
		withCredsNodePubVol    = sp.getEnvBool(ctx, EnvVarCredsNodePubVol)
		withDisableFieldLen    = sp.getEnvBool(ctx, EnvVarDisableFieldLen)
	)

	// Enable all cred requirements if the general option is enabled.
	if withCreds {
		withCredsNewVol = true
		withCredsDelVol = true
		withCredsCtrlrPubVol = true
		withCredsCtrlrUnpubVol = true
		withCredsNodeStgVol = true
		withCredsNodePubVol = true
	}

	// Initialize request & response validation to the global validaiton value.
	var (
		withSpecReq = withSpec
		withSpecRep = withSpec
	)
	log.WithField("withSpec", withSpec).Debug("init req & rep validation")

	// If request validation is not enabled explicitly, check to see if it
	// should be enabled implicitly.
	if !withSpecReq {
		withSpecReq = withCreds ||
			withStgTgtPath ||
			withVolContext ||
			withPubContext
		log.WithField("withSpecReq", withSpecReq).Debug(
			"init implicit req validation")
	}

	// Check to see if spec request or response validation are overridden.
	if v, ok := csictx.LookupEnv(ctx, EnvVarSpecReqValidation); ok {
		withSpecReq, _ = strconv.ParseBool(v)
		log.WithField("withSpecReq", withSpecReq).Debug("init req validation")
	}
	if v, ok := csictx.LookupEnv(ctx, EnvVarSpecRepValidation); ok {
		withSpecRep, _ = strconv.ParseBool(v)
		log.WithField("withSpecRep", withSpecRep).Debug("init rep validation")
	}

	// Configure logging.
	if withReqLogging || withRepLogging {
		// Automatically enable request ID injection if logging
		// is enabled.
		sp.Interceptors = append(sp.Interceptors,
			requestid.NewServerRequestIDInjector())
		log.Debug("enabled request ID injector")

		var (
			loggingOpts []logging.Option
			w           = newLogger(log.Debugf)
		)

		if withDisableLogVolCtx {
			loggingOpts = append(loggingOpts, logging.WithDisableLogVolumeContext())
			log.Debug("disabled logging of VolumeContext field")
		}

		if withReqLogging {
			loggingOpts = append(loggingOpts, logging.WithRequestLogging(w))
			log.Debug("enabled request logging")
		}
		if withRepLogging {
			loggingOpts = append(loggingOpts, logging.WithResponseLogging(w))
			log.Debug("enabled response logging")
		}
		sp.Interceptors = append(sp.Interceptors,
			logging.NewServerLogger(loggingOpts...))
	}

	if withSpecReq || withSpecRep {
		var specOpts []specvalidator.Option

		if withSpecReq {
			specOpts = append(
				specOpts,
				specvalidator.WithRequestValidation())
			log.Debug("enabled spec validator opt: request validation")
		}
		if withSpecRep {
			specOpts = append(
				specOpts,
				specvalidator.WithResponseValidation())
			log.Debug("enabled spec validator opt: response validation")
		}
		if withCredsNewVol {
			specOpts = append(specOpts,
				specvalidator.WithRequiresControllerCreateVolumeSecrets())
			log.Debug("enabled spec validator opt: requires creds: " +
				"CreateVolume")
		}
		if withCredsDelVol {
			specOpts = append(specOpts,
				specvalidator.WithRequiresControllerDeleteVolumeSecrets())
			log.Debug("enabled spec validator opt: requires creds: " +
				"DeleteVolume")
		}
		if withCredsCtrlrPubVol {
			specOpts = append(specOpts,
				specvalidator.WithRequiresControllerPublishVolumeSecrets())
			log.Debug("enabled spec validator opt: requires creds: " +
				"ControllerPublishVolume")
		}
		if withCredsCtrlrUnpubVol {
			specOpts = append(specOpts,
				specvalidator.WithRequiresControllerUnpublishVolumeSecrets())
			log.Debug("enabled spec validator opt: requires creds: " +
				"ControllerUnpublishVolume")
		}
		if withCredsNodeStgVol {
			specOpts = append(specOpts,
				specvalidator.WithRequiresNodeStageVolumeSecrets())
			log.Debug("enabled spec validator opt: requires creds: " +
				"NodeStageVolume")
		}
		if withCredsNodePubVol {
			specOpts = append(specOpts,
				specvalidator.WithRequiresNodePublishVolumeSecrets())
			log.Debug("enabled spec validator opt: requires creds: " +
				"NodePublishVolume")
		}

		if withStgTgtPath {
			specOpts = append(specOpts,
				specvalidator.WithRequiresStagingTargetPath())
			log.Debug("enabled spec validator opt: " +
				"requires starging target path")
		}
		if withVolContext {
			specOpts = append(specOpts,
				specvalidator.WithRequiresVolumeContext())
			log.Debug("enabled spec validator opt: requires vol context")
		}
		if withPubContext {
			specOpts = append(specOpts,
				specvalidator.WithRequiresPublishContext())
			log.Debug("enabled spec validator opt: requires pub context")
		}
		if withDisableFieldLen {
			specOpts = append(specOpts,
				specvalidator.WithDisableFieldLenCheck())
			log.Debug("disabled spec validator opt: field length check")
		}
		sp.Interceptors = append(sp.Interceptors,
			specvalidator.NewServerSpecValidator(specOpts...))
	}

	if _, ok := csictx.LookupEnv(ctx, EnvVarPluginInfo); ok {
		log.Debug("enabled GetPluginInfo interceptor")
		sp.Interceptors = append(sp.Interceptors, sp.getPluginInfo)
	}

	if withSerialVol {
		var (
			opts   []serialvolume.Option
			fields = map[string]interface{}{}
		)

		// Get serial provider's timeout.
		if v, _ := csictx.LookupEnv(
			ctx, EnvVarSerialVolAccessTimeout); v != "" {
			if t, err := time.ParseDuration(v); err == nil {
				fields["serialVol.timeout"] = t
				opts = append(opts, serialvolume.WithTimeout(t))
			}
		}

		// Check for etcd
		if csictx.Getenv(ctx, EnvVarSerialVolAccessEtcdEndpoints) != "" {
			p, err := etcd.New(ctx, "", 0, nil)
			if err != nil {
				log.Fatal(err)
			}
			opts = append(opts, serialvolume.WithLockProvider(p))
		}

		sp.Interceptors = append(sp.Interceptors, serialvolume.New(opts...))
		log.WithFields(fields).Debug("enabled serial volume access")
	}

	return
}

func (sp *StoragePlugin) injectContext(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (interface{}, error) {

	return handler(csictx.WithLookupEnv(ctx, sp.lookupEnv), req)
}

func (sp *StoragePlugin) getPluginInfo(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (interface{}, error) {

	if sp.pluginInfo.Name == "" {
		return handler(ctx, req)
	}

	_, service, method, err := utils.ParseMethod(info.FullMethod)
	if err != nil {
		return nil, err
	}
	if service != "Identity" || method != "GetPluginInfo" {
		return handler(ctx, req)
	}

	return &sp.pluginInfo, nil
}
