package gocsi

const usage = `NAME
    {{.Name}} -- {{.Description}}

SYNOPSIS
    {{.BinPath}}
{{if .Usage}}
STORAGE OPTIONS
{{.Usage}}{{end}}
GLOBAL OPTIONS
    CSI_ENDPOINT
        The CSI endpoint may also be specified by the environment variable
        CSI_ENDPOINT. The endpoint should adhere to Go's network address
        pattern:

            * tcp://host:port
            * unix:///path/to/file.sock.

        If the network type is omitted then the value is assumed to be an
        absolute or relative filesystem path to a UNIX socket file

    X_CSI_MODE
        Specifies the service mode of the storage plug-in. Valid values are:

            * <empty>
            * controller
            * node

        If unset or set to an empty value the storage plug-in activates
        both controller and node services. The identity service is always
        activated.

    X_CSI_ENDPOINT_PERMS
        When CSI_ENDPOINT is set to a UNIX socket file this environment
        variable may be used to specify the socket's file permissions
        as an octal number, ex. 0644. Please note this value has no
        effect if CSI_ENDPOINT specifies a TCP socket.

        The default value is 0755.

    X_CSI_ENDPOINT_USER
        When CSI_ENDPOINT is set to a UNIX socket file this environment
        variable may be used to specify the UID or user name of the
        user that owns the file. Please note this value has no
        effect if CSI_ENDPOINT specifies a TCP socket.

        If no value is specified then the user owner of the file is the
        same as the user that starts the process.

    X_CSI_ENDPOINT_GROUP
        When CSI_ENDPOINT is set to a UNIX socket file this environment
        variable may be used to specify the GID or group name of the
        group that owns the file. Please note this value has no
        effect if CSI_ENDPOINT specifies a TCP socket.

        If no value is specified then the group owner of the file is the
        same as the group that starts the process.

    X_CSI_DEBUG
        Enabling this option is the same as:
            X_CSI_LOG_LEVEL=debug
            X_CSI_REQ_LOGGING=true
            X_CSI_REP_LOGGING=true

    X_CSI_LOG_LEVEL
        The log level. Valid values include:
           * PANIC
           * FATAL
           * ERROR
           * WARN
           * INFO
           * DEBUG

        The default value is WARN.

    X_CSI_PLUGIN_INFO
        The plug-in information is specified via the following
        comma-separated format:

            NAME, VENDOR_VERSION[, MANIFEST...]

        The MANIFEST value may be a series of additional
        comma-separated key/value pairs.

        Please see the encoding/csv package (https://goo.gl/1j1xb9) for
        information on how to quote keys and/or values to include
        leading and trailing whitespace.

        Setting this environment variable will cause the program to
        bypass the SP's GetPluginInfo RPC and returns the specified
        information instead.

    X_CSI_REQ_LOGGING
        A flag that enables logging of incoming requests to STDOUT.

        Enabling this option sets X_CSI_REQ_ID_INJECTION=true.

    X_CSI_REP_LOGGING
        A flag that enables logging of outgoing responses to STDOUT.

        Enabling this option sets X_CSI_REQ_ID_INJECTION=true.

    X_CSI_LOG_DISABLE_VOL_CTX
        A flag that disables the logging of the VolumeContext field.

        Only takes effect if Request or Reply logging is enabled.

    X_CSI_REQ_ID_INJECTION
        A flag that enables request ID injection. The ID is parsed from
        the incoming request's metadata with a key of "csi.requestid".
        If no value for that key is found then a new request ID is
        generated using an atomic sequence counter.

    X_CSI_SPEC_VALIDATION
        Setting X_CSI_SPEC_VALIDATION=true is the same as:
            X_CSI_SPEC_REQ_VALIDATION=true
            X_CSI_SPEC_REP_VALIDATION=true

    X_CSI_SPEC_REQ_VALIDATION
        A flag that enables the validation of CSI request messages.

    X_CSI_SPEC_REP_VALIDATION
        A flag that enables the validation of CSI response messages.
        Invalid responses are marshalled into a gRPC error with a code
        of "Internal."

    X_CSI_SPEC_DISABLE_LEN_CHECK
        A flag that disables validation of CSI message field lengths.

    X_CSI_REQUIRE_STAGING_TARGET_PATH
        A flag that enables treating the following fields as required:
            * NodePublishVolumeRequest.StagingTargetPath

    X_CSI_REQUIRE_VOL_CONTEXT
        A flag that enables treating the following fields as required:
            * ControllerPublishVolumeRequest.VolumeContext
            * ValidateVolumeCapabilitiesRequest.VolumeContext
            * ValidateVolumeCapabilitiesResponse.VolumeContext
            * NodeStageVolumeRequest.VolumeContext
            * NodePublishVolumeRequest.VolumeContext

        Enabling this option sets X_CSI_SPEC_REQ_VALIDATION=true.

    X_CSI_REQUIRE_PUB_CONTEXT
        A flag that enables treating the following fields as required:
            * ControllerPublishVolumeResponse.PublishContext
            * NodeStageVolumeRequest.PublishContext
            * NodePublishVolumeRequest.PublishContext

        Enabling this option sets X_CSI_SPEC_REQ_VALIDATION=true.

    X_CSI_REQUIRE_CREDS
        Setting X_CSI_REQUIRE_CREDS=true is the same as:
            X_CSI_REQUIRE_CREDS_CREATE_VOL=true
            X_CSI_REQUIRE_CREDS_DELETE_VOL=true
            X_CSI_REQUIRE_CREDS_CTRLR_PUB_VOL=true
            X_CSI_REQUIRE_CREDS_CTRLR_UNPUB_VOL=true
            X_CSI_REQUIRE_CREDS_NODE_PUB_VOL=true
            X_CSI_REQUIRE_CREDS_NODE_UNPUB_VOL=true

        Enabling this option sets X_CSI_SPEC_REQ_VALIDATION=true.

    X_CSI_REQUIRE_CREDS_CREATE_VOL
        A flag that enables treating the following fields as required:
            * CreateVolumeRequest.UserCredentials

        Enabling this option sets X_CSI_SPEC_REQ_VALIDATION=true.

    X_CSI_REQUIRE_CREDS_DELETE_VOL
        A flag that enables treating the following fields as required:
            * DeleteVolumeRequest.UserCredentials

        Enabling this option sets X_CSI_SPEC_REQ_VALIDATION=true.

    X_CSI_REQUIRE_CREDS_CTRLR_PUB_VOL
        A flag that enables treating the following fields as required:
            * ControllerPublishVolumeRequest.UserCredentials

        Enabling this option sets X_CSI_SPEC_REQ_VALIDATION=true.

    X_CSI_REQUIRE_CREDS_CTRLR_UNPUB_VOL
        A flag that enables treating the following fields as required:
            * ControllerUnpublishVolumeRequest.UserCredentials

        Enabling this option sets X_CSI_SPEC_REQ_VALIDATION=true.

    X_CSI_REQUIRE_CREDS_NODE_STG_VOL
        A flag that enables treating the following fields as required:
            * NodeStageVolumeRequest.UserCredentials

    X_CSI_REQUIRE_CREDS_NODE_PUB_VOL
        A flag that enables treating the following fields as required:
            * NodePublishVolumeRequest.UserCredentials

        Enabling this option sets X_CSI_SPEC_REQ_VALIDATION=true.

    X_CSI_SERIAL_VOL_ACCESS
        A flag that enables the serial volume access middleware.

    X_CSI_SERIAL_VOL_ACCESS_TIMEOUT
        A time.Duration string that determines how long the serial volume
        access middleware waits to obtain a lock for the request's volume before
        returning a the gRPC error code FailedPrecondition (5) to indicate
        an operation is already pending for the specified volume.

    X_CSI_SERIAL_VOL_ACCESS_ETCD_DOMAIN
        The name of the environment variable that defines the etcd lock
        provider's concurrency domain.

    X_CSI_SERIAL_VOL_ACCESS_ETCD_TTL
        The length of time etcd will wait before  releasing ownership of a
        distributed lock if the lock's session has not been renewed.

    X_CSI_SERIAL_VOL_ACCESS_ETCD_ENDPOINTS
        A comma-separated list of etcd endpoints. If specified then the
        SP's serial volume access middleware will leverage etcd to enable
        distributed locking.

    X_CSI_SERIAL_VOL_ACCESS_ETCD_AUTO_SYNC_INTERVAL
        A time.Duration string that specifies the interval to update
        endpoints with its latest members. A value of 0 disables
        auto-sync. By default auto-sync is disabled.

    X_CSI_SERIAL_VOL_ACCESS_ETCD_DIAL_TIMEOUT
        A time.Duration string that specifies the timeout for failing to
        establish a connection.

    X_CSI_SERIAL_VOL_ACCESS_ETCD_DIAL_KEEP_ALIVE_TIME
        A time.Duration string that defines the time after which the client
        pings the server to see if the transport is alive.

    X_CSI_SERIAL_VOL_ACCESS_ETCD_DIAL_KEEP_ALIVE_TIMEOUT
        A time.Duration string that defines the time that the client waits for
        a response for the keep-alive probe. If the response is not received
        in this time, the connection is closed.

    X_CSI_SERIAL_VOL_ACCESS_ETCD_MAX_CALL_SEND_MSG_SZ
        Defines the client-side request send limit in bytes. If 0, it defaults
        to 2.0 MiB (2 * 1024 * 1024). Make sure that "MaxCallSendMsgSize" <
        server-side default send/recv limit. ("--max-request-bytes" flag to
        etcd or "embed.Config.MaxRequestBytes").

    X_CSI_SERIAL_VOL_ACCESS_ETCD_MAX_CALL_RECV_MSG_SZ
        Defines the client-side response receive limit. If 0, it defaults to
        "math.MaxInt32", because range response can easily exceed request send
        limits. Make sure that "MaxCallRecvMsgSize" >= server-side default
        send/recv limit. ("--max-request-bytes" flag to etcd or
        "embed.Config.MaxRequestBytes").

    X_CSI_SERIAL_VOL_ACCESS_ETCD_USERNAME
        The user name used for authentication.

    X_CSI_SERIAL_VOL_ACCESS_ETCD_PASSWORD
        The password used for authentication.

    X_CSI_SERIAL_VOL_ACCESS_ETCD_REJECT_OLD_CLUSTER
        A flag that indicates refusal to create a client against an outdated
        cluster.

    X_CSI_SERIAL_VOL_ACCESS_ETCD_TLS
        A flag that indicates the client should attempt a TLS connection.

    X_CSI_SERIAL_VOL_ACCESS_ETCD_TLS_INSECURE
        A flag that indicates the TLS connection should not verify peer
        certificates.

The flags -?,-h,-help may be used to print this screen.
`
