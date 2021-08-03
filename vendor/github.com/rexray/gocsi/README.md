# GoCSI
The Container Storage Interface
([CSI](https://github.com/container-storage-interface/spec))
is an industry standard specification for creating storage plug-ins
for container orchestrators. GoCSI aids in the development and testing
of CSI storage plug-ins (SP):

| Component | Description |
|-----------|-------------|
| [csc](./csc/) | CSI command line interface (CLI) client |
| [gocsi](#bootstrapper) | Go-based CSI SP bootstrapper  |
| [mock](./mock) | Mock CSI SP |

## Quick Start
The following example illustrates using Docker in combination with the
GoCSI SP bootstrapper to create a new CSI SP from scratch, serve it on a
UNIX socket, and then use the GoCSI command line client [`csc`](./csc/) to
invoke the `GetPluginInfo` RPC:

```shell
$ docker run -it golang:latest sh -c \
  "go get github.com/rexray/gocsi && \
  make -C src/github.com/rexray/gocsi csi-sp"
```

<a name="bootstrapper"></a>
## Bootstrapping a Storage Plug-in
The root of the GoCSI project enables storage administrators and developers
alike to bootstrap a CSI SP:

```shell
$ ./gocsi.sh
usage: ./gocsi.sh GO_IMPORT_PATH
```

### Bootstrap Example
The GoCSI [Mock SP](./mock) illustrates the features and configuration options
available via the bootstrapping method. The following example demonstrates
creating a new SP at the Go import path `github.com/rexray/csi-sp`:

```shell
$ ./gocsi.sh github.com/rexray/csi-sp
creating project directories:
  /home/akutz/go/src/github.com/rexray/csi-sp
  /home/akutz/go/src/github.com/rexray/csi-sp/provider
  /home/akutz/go/src/github.com/rexray/csi-sp/service
creating project files:
  /home/akutz/go/src/github.com/rexray/csi-sp/main.go
  /home/akutz/go/src/github.com/rexray/csi-sp/provider/provider.go
  /home/akutz/go/src/github.com/rexray/csi-sp/service/service.go
  /home/akutz/go/src/github.com/rexray/csi-sp/service/controller.go
  /home/akutz/go/src/github.com/rexray/csi-sp/service/identity.go
  /home/akutz/go/src/github.com/rexray/csi-sp/service/node.go
use golang/dep? Enter yes (default) or no and press [ENTER]:
  downloading golang/dep@v0.3.2
  executing dep init
building csi-sp:
  success!
  example: CSI_ENDPOINT=csi.sock \
           /home/akutz/go/src/github.com/rexray/csi-sp/csi-sp
```

The new SP adheres to the following structure:

```
|-- provider
|   |
|   |-- provider.go
|
|-- service
|   |
|   |-- controller.go
|   |-- identity.go
|   |-- node.go
|   |-- service.go
|
|-- main.go
```

### Provider
The `provider` package leverages GoCSI to construct an SP from the CSI
services defined in `service` package. The file `provider.go` may be
modified to:

* Supply default values for the SP's environment variable configuration properties

Please see the Mock SP's [`provider.go`](./mock/provider/provider.go) file
for a more complete example.

### Service
The `service` package is where the business logic occurs. The files `controller.go`,
`identity.go`, and `node.go` each correspond to their eponymous CSI services. A
developer creating a new CSI SP with GoCSI will work mostly in these files. Each
of the files have a complete skeleton implementation for their respective service's
remote procedure calls (RPC).

### Main
The root, or `main`, package leverages GoCSI to launch the SP as a stand-alone
server process. The only requirement is that the environment variable `CSI_ENDPOINT`
must be set, otherwise a help screen is emitted that lists all of the SP's available
configuration options (environment variables).

## Configuration
All CSI SPs created using this package are able to leverage the following
environment variables:

<table>
  <thead>
    <tr>
      <th>Name</th>
      <th>Description</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td><code>CSI_ENDPOINT</code></td>
      <td>
        <p>The CSI endpoint may also be specified by the environment variable
        CSI_ENDPOINT. The endpoint should adhere to Go's network address
        pattern:</p>
        <ul>
          <li><code>tcp://host:port</code></li>
          <li><code>unix:///path/to/file.sock</code></li>
        </ul>
        <p>If the network type is omitted then the value is assumed to be an
        absolute or relative filesystem path to a UNIX socket file.</p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_MODE</code></td>
      <td>
        <p>Specifies the service mode of the storage plug-in. Valid
        values are:</p>
        <ul>
          <li><code>&lt;empty&gt;</code></li>
          <li><code>controller</code></li>
          <li><code>node</code></li>
        </ul>
        <p>If unset or set to an empty value the storage plug-in activates
        both controller and node services. The identity service is always
        activated.</p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_ENDPOINT_PERMS</code></td>
      <td>
        <p>When <code>CSI_ENDPOINT</code> is set to a UNIX socket file
        this environment variable may be used to specify the socket's file
        permissions. Please note this value has no effect if
        <code>CSI_ENDPOINT</code> specifies a TCP socket.</p>
        <p>The default value is 0755.</p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_ENDPOINT_USER</code></td>
      <td>
        <p>When <code>CSI_ENDPOINT</code> is set to a UNIX socket file
        this environment variable may be used to specify the UID or name
        of the user that owns the file. Please note this value has no effect
        if <code>CSI_ENDPOINT</code> specifies a TCP socket.</p>
        <p>The default value is the user that starts the process.</p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_ENDPOINT_GROUP</code></td>
      <td>
        <p>When <code>CSI_ENDPOINT</code> is set to a UNIX socket file
        this environment variable may be used to specify the GID or name
        of the group that owns the file. Please note this value has no effect
        if <code>CSI_ENDPOINT</code> specifies a TCP socket.</p>
        <p>The default value is the group that starts the process.</p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_DEBUG</code></td>
      <td>A <code>true</code> value is equivalent to:
        <ul>
          <li><code>X_CSI_LOG_LEVEL=debug</code></li>
          <li><code>X_CSI_REQ_LOGGING=true</code></li>
          <li><code>X_CSI_REP_LOGGING=true</code></li>
        </ul>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_LOG_LEVEL</code></td>
      <td>
        <p>The log level. Valid values include:</p>
        <ul>
          <li><code>PANIC</code></li>
          <li><code>FATAL</code></li>
          <li><code>ERROR</code></li>
          <li><code>WARN</code></li>
          <li><code>INFO</code></li>
          <li><code>DEBUG</code></li>
        </ul>
        <p>The default value is <code>WARN</code>.</p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_REQ_LOGGING</code></td>
      <td><p>A flag that enables logging of incoming requests to
      <code>STDOUT</code>.</p>
      <p>Enabling this option sets <code>X_CSI_REQ_ID_INJECTION=true</code>.</p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_REP_LOGGING</code></td>
      <td><p>A flag that enables logging of incoming responses to
      <code>STDOUT</code>.</p>
      <p>Enabling this option sets <code>X_CSI_REQ_ID_INJECTION=true</code>.</p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_LOG_DISABLE_VOL_CTX</code></td>
      <td><p>A flag that disables the logging of the VolumeContext field.</p>
      <p>Only takes effect if Request or Reply logging is enabled.</p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_REQ_ID_INJECTION</code></td>
      <td>A flag that enables request ID injection. The ID is parsed from
      the incoming request's metadata with a key of
      <code>csi.requestid</code>.
      If no value for that key is found then a new request ID is
      generated using an atomic sequence counter.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SPEC_VALIDATION</code></td>
      <td>Setting <code>X_CSI_SPEC_VALIDATION=true</code> is the same as:
        <ul>
          <li><code>X_CSI_SPEC_REQ_VALIDATION=true</code></li>
          <li><code>X_CSI_SPEC_REP_VALIDATION=true</code></li>
        </ul>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_SPEC_REQ_VALIDATION</code></td>
      <td>A flag that enables the validation of CSI request messages.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SPEC_REP_VALIDATION</code></td>
      <td>A flag that enables the validation of CSI response messages.
      Invalid responses are marshalled into a gRPC error with a code
      of <code>Internal</code>.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SPEC_DISABLE_LEN_CHECK</code></td>
      <td>A flag that disables validation of CSI message field lengths.</td>
    </tr>
    <tr>
      <td><code>X_CSI_REQUIRE_STAGING_TARGET_PATH</code></td>
      <td>
        <p>A flag that enables treating the following fields as required:</p>
        <ul>
          <li><code>NodePublishVolumeRequest.StagingTargetPath</code></li>
      </ul>
      <p>Enabling this option sets <code>X_CSI_SPEC_REQ_VALIDATION=true</code></p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_REQUIRE_VOL_CONTEXT</code></td>
      <td>
        <p>A flag that enables treating the following fields as required:</p>
        <ul>
          <li><code>ControllerPublishVolumeRequest.VolumeContext</code></li>
          <li><code>ValidateVolumeCapabilitiesRequest.VolumeContext</code></li>
          <li><code>ValidateVolumeCapabilitiesResponse.VolumeContext</code></li>
          <li><code>NodeStageVolumeRequest.VolumeContext</code></li>
          <li><code>NodePublishVolumeRequest.VolumeContext</code></li>
        </ul>
        <p>Enabling this option sets <code>X_CSI_SPEC_REQ_VALIDATION=true</code></p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_REQUIRE_PUB_CONTEXT</code></td>
      <td>
        <p>A flag that enables treating the following fields as required:</p>
        <ul>
          <li><code>ControllerPublishVolumeResponse.PublishContext</code></li>
          <li><code>NodeStageVolumeRequest.PublishContext</code></li>
          <li><code>NodePublishVolumeRequest.PublishContext</code></li>
        </ul>
        <p>Enabling this option sets <code>X_CSI_SPEC_REQ_VALIDATION=true</code></p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_REQUIRE_CREDS</code></td>
      <td>A <code>true</code> value is equivalent to:
        <ul>
          <li><code>X_CSI_REQUIRE_CREDS_CREATE_VOL=true</code></li>
          <li><code>X_CSI_REQUIRE_CREDS_DELETE_VOL=true</code></li>
          <li><code>X_CSI_REQUIRE_CREDS_CTRLR_PUB_VOL=true</code></li>
          <li><code>X_CSI_REQUIRE_CREDS_CTRLR_UNPUB_VOL=true</code></li>
          <li><code>X_CSI_REQUIRE_CREDS_NODE_PUB_VOL=true</code></li>
          <li><code>X_CSI_REQUIRE_CREDS_NODE_UNPUB_VOL=true</code></li>
        </ul>
        <p>Enabling this option sets <code>X_CSI_SPEC_REQ_VALIDATION=true</code></p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_REQUIRE_CREDS_CREATE_VOL</code></td>
      <td>
        <p>A flag that enables treating the following fields as required:</p>
        <ul><li><code>CreateVolumeRequest.UserCredentials</code></li></ul>
        <p>Enabling this option sets <code>X_CSI_SPEC_REQ_VALIDATION=true</code></p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_REQUIRE_CREDS_DELETE_VOL</code></td>
      <td>
        <p>A flag that enables treating the following fields as required:</p>
        <ul><li><code>DeleteVolumeRequest.UserCredentials</code></li></ul>
        <p>Enabling this option sets <code>X_CSI_SPEC_REQ_VALIDATION=true</code></p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_REQUIRE_CREDS_CTRLR_PUB_VOL</code></td>
      <td>
        <p>A flag that enables treating the following fields as required:</p>
        <ul><li><code>ControllerPublishVolumeRequest.UserCredentials</code></li></ul>
        <p>Enabling this option sets <code>X_CSI_SPEC_REQ_VALIDATION=true</code></p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_REQUIRE_CREDS_CTRLR_UNPUB_VOL</code></td>
      <td>
        <p>A flag that enables treating the following fields as required:</p>
        <ul><li><code>ControllerUnpublishVolumeRequest.UserCredentials</code></li></ul>
        <p>Enabling this option sets <code>X_CSI_SPEC_REQ_VALIDATION=true</code></p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_REQUIRE_CREDS_NODE_STG_VOL</code></td>
      <td>
        <p>A flag that enables treating the following fields as required:</p>
        <ul><li><code>NodeStageVolumeRequest.UserCredentials</code></li></ul>
        <p>Enabling this option sets <code>X_CSI_SPEC_REQ_VALIDATION=true</code></p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_REQUIRE_CREDS_NODE_PUB_VOL</code></td>
      <td>
        <p>A flag that enables treating the following fields as required:</p>
        <ul><li><code>NodePublishVolumeRequest.UserCredentials</code></li></ul>
        <p>Enabling this option sets <code>X_CSI_SPEC_REQ_VALIDATION=true</code></p>
      </td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS</code></td>
      <td>A flag that enables the serial volume access middleware.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_TIMEOUT</code></td>
      <td>A <a href="https://golang.org/pkg/time/#ParseDuration"><code>
      time.Duration</code></a> string that determines how long the
      serial volume access middleware waits to obtain a lock for the request's
      volume before returning the gRPC error code <code>FailedPrecondition</code> to
      indicate an operation is already pending for the specified volume.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_ENDPOINTS</code></td>
      <td>A list comma-separated etcd endpoint values. If this environment
      variable is defined then the serial volume access middleware will
      automatically use etcd for locking, providing distributed serial
      volume access.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_DOMAIN</code></td>
      <td>The etcd key prefix to use with the locks that provide
      distributed, serial volume access. The key paths are:
      <ul>
        <li><code>/DOMAIN/volumesByID/VOLUME_ID</code></li>
        <li><code>/DOMAIN/volumesByName/VOLUME_NAME</code></li>
      </ul></td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_TTL</code></td>
      <td>The length of time etcd will wait before  releasing ownership of
      a distributed lock if the lock's session has not been renewed.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_AUTO_SYNC_INTERVAL</code></td>
      <td>A time.Duration string that specifies the interval to update
      endpoints with its latest members. A value of 0 disables
      auto-sync. By default auto-sync is disabled.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_DIAL_TIMEOUT</code></td>
      <td>A time.Duration string that specifies the timeout for failing to
      establish a connection.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_DIAL_KEEP_ALIVE_TIME</code></td>
      <td>A time.Duration string that defines the time after which the client
      pings the server to see if the transport is alive.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_DIAL_KEEP_ALIVE_TIMEOUT</code></td>
      <td>A time.Duration string that defines the time that the client waits for
      a response for the keep-alive probe. If the response is not received
      in this time, the connection is closed.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_MAX_CALL_SEND_MSG_SZ</code></td>
      <td>Defines the client-side request send limit in bytes. If 0, it defaults
      to 2.0 MiB (2 * 1024 * 1024). Make sure that "MaxCallSendMsgSize" <
      server-side default send/recv limit. ("--max-request-bytes" flag to
      etcd or "embed.Config.MaxRequestBytes").</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_MAX_CALL_RECV_MSG_SZ</code></td>
      <td>Defines the client-side response receive limit. If 0, it defaults to
      "math.MaxInt32", because range response can easily exceed request send
      limits. Make sure that "MaxCallRecvMsgSize" >= server-side default
      send/recv limit. ("--max-request-bytes" flag to etcd or
      "embed.Config.MaxRequestBytes").</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_USERNAME</code></td>
      <td>The user name used for authentication.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_PASSWORD</code></td>
      <td>The password used for authentication.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_REJECT_OLD_CLUSTER</code></td>
      <td>A flag that indicates refusal to create a client against an outdated
      cluster.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_TLS</code></td>
      <td>A flag that indicates the client should use TLS.</td>
    </tr>
    <tr>
      <td><code>X_CSI_SERIAL_VOL_ACCESS_ETCD_TLS_INSECURE</code></td>
      <td>A flag that indicates the TLS connection should not verify peer
      certificates.</td>
    </tr>
  </tbody>
</table>
