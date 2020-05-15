package etcd

const (
	// EnvVarDomain is the name of the environment variable that defines
	// the lock provider's concurrency domain.
	EnvVarDomain = "X_CSI_SERIAL_VOL_ACCESS_ETCD_DOMAIN"

	// EnvVarEndpoints is the name of the environment variable that defines
	// the lock provider's etcd endoints.
	EnvVarEndpoints = "X_CSI_SERIAL_VOL_ACCESS_ETCD_ENDPOINTS"

	// EnvVarTTL is the name of the environment
	// variable that defines the length of time etcd will wait before
	// releasing ownership of a distributed lock if the lock's session
	// has not been renewed.
	EnvVarTTL = "X_CSI_SERIAL_VOL_ACCESS_ETCD_TTL"

	// EnvVarAutoSyncInterval is the name of the environment
	// variable that defines the interval to update endpoints with its latest
	//  members. 0 disables auto-sync. By default auto-sync is disabled.
	EnvVarAutoSyncInterval = "X_CSI_SERIAL_VOL_ACCESS_ETCD_AUTO_SYNC_INTERVAL"

	// EnvVarDialTimeout is the name of the environment
	// variable that defines the timeout for failing to establish a connection.
	EnvVarDialTimeout = "X_CSI_SERIAL_VOL_ACCESS_ETCD_DIAL_TIMEOUT"

	// EnvVarDialKeepAliveTime is the name of the environment
	// variable that defines the time after which client pings the server to see
	// if transport is alive.
	EnvVarDialKeepAliveTime = "X_CSI_SERIAL_VOL_ACCESS_ETCD_DIAL_KEEP_ALIVE_TIME"

	// EnvVarDialKeepAliveTimeout is the name of the
	// environment variable that defines the time that the client waits for a
	// response for the keep-alive probe. If the response is not received in
	// this time, the connection is closed.
	EnvVarDialKeepAliveTimeout = "X_CSI_SERIAL_VOL_ACCESS_ETCD_DIAL_KEEP_ALIVE_TIMEOUT"

	// EnvVarMaxCallSendMsgSz is the name of the environment
	// variable that defines the client-side request send limit in bytes.
	// If 0, it defaults to 2.0 MiB (2 * 1024 * 1024).
	// Make sure that "MaxCallSendMsgSize" < server-side default send/recv
	// limit. ("--max-request-bytes" flag to etcd or
	// "embed.Config.MaxRequestBytes").
	EnvVarMaxCallSendMsgSz = "X_CSI_SERIAL_VOL_ACCESS_ETCD_MAX_CALL_SEND_MSG_SZ"

	// EnvVarMaxCallRecvMsgSz is the name of the environment
	// variable that defines the client-side response receive limit.
	// If 0, it defaults to "math.MaxInt32", because range response can
	// easily exceed request send limits.
	// Make sure that "MaxCallRecvMsgSize" >= server-side default send/recv
	// limit. ("--max-request-bytes" flag to etcd or
	// "embed.Config.MaxRequestBytes").
	EnvVarMaxCallRecvMsgSz = "X_CSI_SERIAL_VOL_ACCESS_ETCD_MAX_CALL_RECV_MSG_SZ"

	// EnvVarUsername is the name of the environment
	// variable that defines the user name used for authentication.
	EnvVarUsername = "X_CSI_SERIAL_VOL_ACCESS_ETCD_USERNAME"

	// EnvVarPassword is the name of the environment
	// variable that defines the password used for authentication.
	EnvVarPassword = "X_CSI_SERIAL_VOL_ACCESS_ETCD_PASSWORD"

	// EnvVarRejectOldCluster is the name of the environment
	// variable that defines when set will refuse to create a client against
	// an outdated cluster.
	EnvVarRejectOldCluster = "X_CSI_SERIAL_VOL_ACCESS_ETCD_REJECT_OLD_CLUSTER"

	// EnvVarTLS is the name of the environment
	// variable that defines whether or not the client should attempt
	// to use TLS when connecting to the server.
	EnvVarTLS = "X_CSI_SERIAL_VOL_ACCESS_ETCD_TLS"

	// EnvVarTLSInsecure is the name of the environment
	// variable that defines whether or not the TLS connection should
	// verify certificates.
	EnvVarTLSInsecure = "X_CSI_SERIAL_VOL_ACCESS_ETCD_TLS_INSECURE"
)
