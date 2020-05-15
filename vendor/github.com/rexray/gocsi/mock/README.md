# Mock Plug-in
The mock plug-in is a stand-alone binary that implements the CSI
Controller, Identity, and Node RPCs in addition to the specification's
requirements regarding idempotency.

The mock plug-in always starts with a deterministic state and maintains
state for the duration of  the process. The state can also be modified.
For example, while the plug-in launches with three volumes, a
`CreateVolume` RPC will update the plug-in's internal data map so that a
subsequent `ListVolumes` RPC will indicate four volumes are present.

Per the specification the Mock plug-in starts a gRPC server using the
value of the environment variable `CSI_ENDPOINT`. The plug-in process
runs in the foreground, logging activity to `STDOUT` and errors to
`STDERR`, only returning control to the user when `CTRL-C` is entered
or the process is sent a `kill` signal.

```bash
$ CSI_ENDPOINT=/tmp/csi.sock mock/mock
INFO  2017/08/22 16:22:15 main.go:154: mock.Serve: /tmp/csi.sock
INFO  2017/08/22 16:22:18 main.go:133: /csi.Controller/CreateVolume: REQ 0001: Version=minor:1 , Name=Test Volume, CapacityRange=required_bytes:10740000000 limit_bytes:107400000000 , VolumeCapabilities=[mount:<fs_type:"ext4" mount_flags:"-o noexec" > ], Parameters=map[tag:gold]
INFO  2017/08/22 16:22:18 main.go:133: /csi.Controller/CreateVolume: REP 0001: Reply=&{volume_info:<capacity_bytes:107400000000 id:<values:<key:"id" value:"4" > values:<key:"name" value:"Test Volume" > > metadata:<> > }
INFO  2017/08/22 16:23:53 main.go:94: received signal: interrupt: shutting down
INFO  2017/08/22 16:23:53 main.go:188: mock.GracefulStop
INFO  2017/08/22 16:23:53 main.go:53: removed sock file: /tmp/csi.sock
INFO  2017/08/22 16:23:53 main.go:64: server stopped gracefully
```

## Configuration
The Mock CSI plug-in is created using the GoCSI CSP package. Please
see its [configuration section](../csp/README.md#configuration) for
a complete list of the environment variables that may be used to
configure the Mock SP.

The following table is a list of the Mock SP's default configuration
values:

| Name | Value |
|------|---------|
| `X_CSI_REQUIRE_PUB_CONTEXT` | `true` |
| `X_CSI_SERIAL_VOL_ACCESS` | `true` |
| `X_CSI_SPEC_VALIDATION` | `true` |
