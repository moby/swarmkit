package gocsi

import (
	"context"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	csictx "github.com/rexray/gocsi/context"
	"github.com/rexray/gocsi/utils"
)

const (
	// EnvVarEndpoint is the name of the environment variable used to
	// specify the CSI endpoint.
	EnvVarEndpoint = "CSI_ENDPOINT"

	// EnvVarEndpointPerms is the name of the environment variable used
	// to specify the file permissions for the CSI endpoint when it is
	// a UNIX socket file. This setting has no effect if CSI_ENDPOINT
	// specifies a TCP socket. The default value is 0755.
	EnvVarEndpointPerms = "X_CSI_ENDPOINT_PERMS"

	// EnvVarEndpointUser is the name of the environment variable used
	// to specify the UID or name of the user that owns the endpoint's
	// UNIX socket file. This setting has no effect if CSI_ENDPOINT
	// specifies a TCP socket. The default value is the user that starts
	// the process.
	EnvVarEndpointUser = "X_CSI_ENDPOINT_USER"

	// EnvVarEndpointGroup is the name of the environment variable used
	// to specify the GID or name of the group that owns the endpoint's
	// UNIX socket file. This setting has no effect if CSI_ENDPOINT
	// specifies a TCP socket. The default value is the group that starts
	// the process.
	EnvVarEndpointGroup = "X_CSI_ENDPOINT_GROUP"

	// EnvVarDebug is the name of the environment variable used to
	// determine whether or not debug mode is enabled.
	//
	// Setting this environment variable to a truthy value is the
	// equivalent of X_CSI_LOG_LEVEL=DEBUG, X_CSI_REQ_LOGGING=true,
	// and X_CSI_REP_LOGGING=true.
	EnvVarDebug = "X_CSI_DEBUG"

	// EnvVarLogLevel is the name of the environment variable used to
	// specify the log level. Valid values include PANIC, FATAL, ERROR,
	// WARN, INFO, and DEBUG.
	EnvVarLogLevel = "X_CSI_LOG_LEVEL"

	// EnvVarPluginInfo is the name of the environment variable used to
	// specify the plug-in info in the format:
	//
	//         NAME, VENDOR_VERSION[, MANIFEST...]
	//
	// The MANIFEST value may be a series of additional comma-separated
	// key/value pairs.
	//
	// Please see the encoding/csv package (https://goo.gl/1j1xb9) for
	// information on how to quote keys and/or values to include leading
	// and trailing whitespace.
	//
	// Setting this environment variable will cause the program to
	// bypass the SP's GetPluginInfo RPC and returns the specified
	// information instead.
	EnvVarPluginInfo = "X_CSI_PLUGIN_INFO"

	// EnvVarMode is the name of the environment variable used to specify
	// the service mode of the storage plug-in. Valie values are:
	//
	// * <empty>
	// * controller
	// * node
	//
	// If unset or set to an empty value the storage plug-in activates
	// both controller and node services. The identity service is always
	// activated.
	EnvVarMode = "X_CSI_MODE"

	// EnvVarReqLogging is the name of the environment variable
	// used to determine whether or not to enable request logging.
	//
	// Setting this environment variable to a truthy value enables
	// request logging to STDOUT.
	EnvVarReqLogging = "X_CSI_REQ_LOGGING"

	// EnvVarRepLogging is the name of the environment variable
	// used to determine whether or not to enable response logging.
	//
	// Setting this environment variable to a truthy value enables
	// response logging to STDOUT.
	EnvVarRepLogging = "X_CSI_REP_LOGGING"

	// EnvVarLoggingDisableVolCtx is the name of the environment variable
	// used to disable the logging of the VolumeContext field when request or
	// response logging is enabled.
	//
	// Setting this environment variable to a truthy value disables the logging
	// of the VolumeContext field
	EnvVarLoggingDisableVolCtx = "X_CSI_LOG_DISABLE_VOL_CTX"

	// EnvVarReqIDInjection is the name of the environment variable
	// used to determine whether or not to enable request ID injection.
	EnvVarReqIDInjection = "X_CSI_REQ_ID_INJECTION"

	// EnvVarSpecValidation is the name of the environment variable
	// used to determine whether or not to enable validation of CSI
	// request and response messages. Setting X_CSI_SPEC_VALIDATION=true
	// is the equivalent to setting X_CSI_SPEC_REQ_VALIDATION=true and
	// X_CSI_SPEC_REP_VALIDATION=true.
	EnvVarSpecValidation = "X_CSI_SPEC_VALIDATION"

	// EnvVarSpecReqValidation is the name of the environment variable
	// used to determine whether or not to enable validation of CSI request
	// messages.
	EnvVarSpecReqValidation = "X_CSI_SPEC_REQ_VALIDATION"

	// EnvVarSpecRepValidation is the name of the environment variable
	// used to determine whether or not to enable validation of CSI response
	// messages. Invalid responses are marshalled into a gRPC error with
	// a code of "Internal."
	EnvVarSpecRepValidation = "X_CSI_SPEC_REP_VALIDATION"

	// EnvVarDisableFieldLen is the name of the environment variable used
	// to determine whether or not to disable validation of CSI request and
	// response field lengths against the permitted lenghts defined in the spec
	EnvVarDisableFieldLen = "X_CSI_SPEC_DISABLE_LEN_CHECK"

	// EnvVarRequireStagingTargetPath is the name of the environment variable
	// used to determine whether or not the NodePublishVolume request field
	// StagingTargetPath is required.
	EnvVarRequireStagingTargetPath = "X_CSI_REQUIRE_STAGING_TARGET_PATH"

	// EnvVarRequireVolContext is the name of the environment variable used
	// to determine whether or not volume context is required for
	// requests that accept it and responses that return it such as
	// NodePublishVolume and ControllerPublishVolume.
	EnvVarRequireVolContext = "X_CSI_REQUIRE_VOL_CONTEXT"

	// EnvVarRequirePubContext is the name of the environment variable used
	// to determine whether or not publish context is required for
	// requests that accept it and responses that return it such as
	// NodePublishVolume and ControllerPublishVolume.
	EnvVarRequirePubContext = "X_CSI_REQUIRE_PUB_CONTEXT"

	// EnvVarCreds is the name of the environment variable
	// used to determine whether or not user credentials are required for
	// all RPCs. This value may be overridden for specific RPCs.
	EnvVarCreds = "X_CSI_REQUIRE_CREDS"

	// EnvVarCredsCreateVol is the name of the environment variable
	// used to determine whether or not user credentials are required for
	// the eponymous RPC.
	EnvVarCredsCreateVol = "X_CSI_REQUIRE_CREDS_CREATE_VOL"

	// EnvVarCredsDeleteVol is the name of the environment variable
	// used to determine whether or not user credentials are required for
	// the eponymous RPC.
	EnvVarCredsDeleteVol = "X_CSI_REQUIRE_CREDS_DELETE_VOL"

	// EnvVarCredsCtrlrPubVol is the name of the environment
	// variable used to determine whether or not user credentials are required
	// for the eponymous RPC.
	EnvVarCredsCtrlrPubVol = "X_CSI_REQUIRE_CREDS_CTRLR_PUB_VOL"

	// EnvVarCredsCtrlrUnpubVol is the name of the
	// environment variable used to determine whether or not user credentials
	// are required for the eponymous RPC.
	EnvVarCredsCtrlrUnpubVol = "X_CSI_REQUIRE_CREDS_CTRLR_UNPUB_VOL"

	// EnvVarCredsNodeStgVol is the name of the environment
	// variable used to determine whether or not user credentials are required
	// for the eponymous RPC.
	EnvVarCredsNodeStgVol = "X_CSI_REQUIRE_CREDS_NODE_STG_VOL"

	// EnvVarCredsNodePubVol is the name of the environment
	// variable used to determine whether or not user credentials are required
	// for the eponymous RPC.
	EnvVarCredsNodePubVol = "X_CSI_REQUIRE_CREDS_NODE_PUB_VOL"

	// EnvVarSerialVolAccess is the name of the environment variable
	// used to determine whether or not to enable serial volume access.
	EnvVarSerialVolAccess = "X_CSI_SERIAL_VOL_ACCESS"

	// EnvVarSerialVolAccessTimeout is the name of the environment variable
	// used to specify the timeout for obtaining a volume lock.
	EnvVarSerialVolAccessTimeout = "X_CSI_SERIAL_VOL_ACCESS_TIMEOUT"

	// EnvVarSerialVolAccessEtcdDomain is the name of the environment
	// variable that defines the lock provider's concurrency domain.
	EnvVarSerialVolAccessEtcdDomain = "X_CSI_SERIAL_VOL_ACCESS_ETCD_DOMAIN"

	// EnvVarSerialVolAccessEtcdTTL is the name of the environment
	// variable that defines the length of time etcd will wait before
	// releasing ownership of a distributed lock if the lock's session
	// has not been renewed.
	EnvVarSerialVolAccessEtcdTTL = "X_CSI_SERIAL_VOL_ACCESS_ETCD_TTL"

	// EnvVarSerialVolAccessEtcdEndpoints is the name of the environment
	// variable that defines the lock provider's etcd endoints.
	EnvVarSerialVolAccessEtcdEndpoints = "X_CSI_SERIAL_VOL_ACCESS_ETCD_ENDPOINTS"

	// EnvVarSerialVolAccessEtcdAutoSyncInterval is the name of the environment
	// variable that defines the interval to update endpoints with its latest
	//  members. 0 disables auto-sync. By default auto-sync is disabled.
	EnvVarSerialVolAccessEtcdAutoSyncInterval = "X_CSI_SERIAL_VOL_ACCESS_ETCD_AUTO_SYNC_INTERVAL"

	// EnvVarSerialVolAccessEtcdDialTimeout is the name of the environment
	// variable that defines the timeout for failing to establish a connection.
	EnvVarSerialVolAccessEtcdDialTimeout = "X_CSI_SERIAL_VOL_ACCESS_ETCD_DIAL_TIMEOUT"

	// EnvVarSerialVolAccessEtcdDialKeepAliveTime is the name of the environment
	// variable that defines the time after which client pings the server to see
	// if transport is alive.
	EnvVarSerialVolAccessEtcdDialKeepAliveTime = "X_CSI_SERIAL_VOL_ACCESS_ETCD_DIAL_KEEP_ALIVE_TIME"

	// EnvVarSerialVolAccessEtcdDialKeepAliveTimeout is the name of the
	// environment variable that defines the time that the client waits for a
	// response for the keep-alive probe. If the response is not received in
	// this time, the connection is closed.
	EnvVarSerialVolAccessEtcdDialKeepAliveTimeout = "X_CSI_SERIAL_VOL_ACCESS_ETCD_DIAL_KEEP_ALIVE_TIMEOUT"

	// EnvVarSerialVolAccessEtcdMaxCallSendMsgSz is the name of the environment
	// variable that defines the client-side request send limit in bytes.
	// If 0, it defaults to 2.0 MiB (2 * 1024 * 1024).
	// Make sure that "MaxCallSendMsgSize" < server-side default send/recv
	// limit. ("--max-request-bytes" flag to etcd or
	// "embed.Config.MaxRequestBytes").
	EnvVarSerialVolAccessEtcdMaxCallSendMsgSz = "X_CSI_SERIAL_VOL_ACCESS_ETCD_MAX_CALL_SEND_MSG_SZ"

	// EnvVarSerialVolAccessEtcdMaxCallRecvMsgSz is the name of the environment
	// variable that defines the client-side response receive limit.
	// If 0, it defaults to "math.MaxInt32", because range response can
	// easily exceed request send limits.
	// Make sure that "MaxCallRecvMsgSize" >= server-side default send/recv
	// limit. ("--max-request-bytes" flag to etcd or
	// "embed.Config.MaxRequestBytes").
	EnvVarSerialVolAccessEtcdMaxCallRecvMsgSz = "X_CSI_SERIAL_VOL_ACCESS_ETCD_MAX_CALL_RECV_MSG_SZ"

	// EnvVarSerialVolAccessEtcdUsername is the name of the environment
	// variable that defines the user name used for authentication.
	EnvVarSerialVolAccessEtcdUsername = "X_CSI_SERIAL_VOL_ACCESS_ETCD_USERNAME"

	// EnvVarSerialVolAccessEtcdPassword is the name of the environment
	// variable that defines the password used for authentication.
	EnvVarSerialVolAccessEtcdPassword = "X_CSI_SERIAL_VOL_ACCESS_ETCD_PASSWORD"

	// EnvVarSerialVolAccessEtcdRejectOldCluster is the name of the environment
	// variable that defines when set will refuse to create a client against
	// an outdated cluster.
	EnvVarSerialVolAccessEtcdRejectOldCluster = "X_CSI_SERIAL_VOL_ACCESS_ETCD_REJECT_OLD_CLUSTER"

	// EnvVarSerialVolAccessEtcdTLS is the name of the environment
	// variable that defines whether or not the client should attempt
	// to use TLS when connecting to the server.
	EnvVarSerialVolAccessEtcdTLS = "X_CSI_SERIAL_VOL_ACCESS_ETCD_TLS"

	// EnvVarSerialVolAccessEtcdTLSInsecure is the name of the environment
	// variable that defines whether or not the TLS connection should
	// verify certificates.
	EnvVarSerialVolAccessEtcdTLSInsecure = "X_CSI_SERIAL_VOL_ACCESS_ETCD_TLS_INSECURE"
)

func (sp *StoragePlugin) initEnvVars(ctx context.Context) {

	// Copy the environment variables from the public EnvVar
	// string slice to the private envVars map for quick lookup.
	sp.envVars = map[string]string{}
	for _, v := range sp.EnvVars {
		// Environment variables must adhere to one of the following
		// formats:
		//
		//     - ENV_VAR_KEY=
		//     - ENV_VAR_KEY=ENV_VAR_VAL
		pair := strings.SplitN(v, "=", 2)
		if len(pair) < 1 || len(pair) > 2 {
			continue
		}

		// Ensure the environment variable is stored in all upper-case
		// to make subsequent map-lookups deterministic.
		key := strings.ToUpper(pair[0])

		// Check to see if the value for the key is available from the
		// context's os.Environ or os.LookupEnv functions. If neither
		// return a value then use the provided default value.
		var val string
		if v, ok := csictx.LookupEnv(ctx, key); ok {
			val = v
		} else if len(pair) > 1 {
			val = pair[1]
		}
		sp.envVars[key] = val
	}

	// Check for the debug value.
	if v, ok := csictx.LookupEnv(ctx, EnvVarDebug); ok {
		if ok, _ := strconv.ParseBool(v); ok {
			csictx.Setenv(ctx, EnvVarReqLogging, "true")
			csictx.Setenv(ctx, EnvVarRepLogging, "true")
		}
	}

	return
}

func (sp *StoragePlugin) initPluginInfo(ctx context.Context) {
	szInfo, ok := csictx.LookupEnv(ctx, EnvVarPluginInfo)
	if !ok {
		return
	}
	info := strings.SplitN(szInfo, ",", 3)
	fields := map[string]interface{}{}
	if len(info) > 0 {
		sp.pluginInfo.Name = strings.TrimSpace(info[0])
		fields["name"] = sp.pluginInfo.Name
	}
	if len(info) > 1 {
		sp.pluginInfo.VendorVersion = strings.TrimSpace(info[1])
		fields["vendorVersion"] = sp.pluginInfo.VendorVersion
	}
	if len(info) > 2 {
		sp.pluginInfo.Manifest = utils.ParseMap(strings.TrimSpace(info[2]))
		fields["manifest"] = sp.pluginInfo.Manifest
	}

	if len(fields) > 0 {
		log.WithFields(fields).Debug("init plug-in info")
	}
}
