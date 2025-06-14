module github.com/moby/swarmkit/swarmd

go 1.18

require (
	github.com/cloudflare/cfssl v1.6.4
	github.com/docker/docker v24.0.0-rc.2.0.20230908212318-6ce5aa1cd5a4+incompatible // master (v25.0.0-dev)
	github.com/docker/go-connections v0.4.1-0.20231110212414-fa09c952e3ea
	github.com/docker/go-units v0.5.0
	github.com/dustin/go-humanize v1.0.0
	github.com/gogo/protobuf v1.3.2
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/moby/swarmkit/v2 v2.0.0-20240125134710-dcda100a8261
	github.com/opencontainers/image-spec v1.1.0-rc5
	github.com/prometheus/client_golang v1.14.0
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/cobra v1.7.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.8.4
	go.etcd.io/etcd/client/pkg/v3 v3.5.6
	go.etcd.io/etcd/raft/v3 v3.5.6
	go.etcd.io/etcd/server/v3 v3.5.6
	golang.org/x/time v0.3.0

	// NOTE(dperny,cyli): there is some error handling, found in the
	// (*firstSessionErrorTracker).SessionClosed method in node/node.go, which
	// relies on string matching to handle x509 errors. between grpc versions 1.3.0
	// and 1.7.5, the error string we were matching changed, breaking swarmkit.
	// In 1.10.x, GRPC stopped surfacing those errors entirely, breaking swarmkit.
	// In >=1.11, those errors were brought back but the string had changed again.
	// After updating GRPC, if integration test failures occur, verify that the
	// string matching there is correct.
	//
	// For details, see:
	//
	// - https://github.com/moby/swarmkit/commit/4343384f11737119c3fa1524da2cb2707c70e04a
	// - https://github.com/moby/swarmkit/commit/8a2b6fd64944bcef8154ced28f90aeec6abfeb04
	google.golang.org/grpc v1.53.0
)

require (
	code.cloudfoundry.org/clock v1.1.0 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20210617225240-d185dfc1b5a1 // indirect
	github.com/Microsoft/go-winio v0.6.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/container-storage-interface/spec v1.2.0 // indirect
	github.com/containerd/containerd v1.6.22 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/distribution/reference v0.5.0 // indirect
	github.com/docker/go-events v0.0.0-20190806004212-e31b211e4f1c // indirect
	github.com/docker/go-metrics v0.0.1 // indirect
	github.com/felixge/httpsnoop v1.0.3 // indirect
	github.com/fernet/fernet-go v0.0.0-20211208181803-9f70042a33ee // indirect
	github.com/go-logr/logr v1.2.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/certificate-transparency-go v1.1.4 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/go-memdb v1.3.2 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/ishidawataru/sctp v0.0.0-20230406120618-7ff4192f6ff2 // indirect
	github.com/jmoiron/sqlx v1.3.3 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.3.0 // indirect
	github.com/prometheus/common v0.42.0 // indirect
	github.com/prometheus/procfs v0.9.0 // indirect
	github.com/weppos/publicsuffix-go v0.15.1-0.20210511084619-b1f36a2d6c0b // indirect
	github.com/zmap/zcrypto v0.0.0-20210511125630-18f1e0152cfc // indirect
	github.com/zmap/zlint/v3 v3.1.0 // indirect
	go.etcd.io/bbolt v1.3.7 // indirect
	go.etcd.io/etcd/pkg/v3 v3.5.6 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.29.0 // indirect
	go.opentelemetry.io/otel v1.4.1 // indirect
	go.opentelemetry.io/otel/internal/metric v0.27.0 // indirect
	go.opentelemetry.io/otel/metric v0.27.0 // indirect
	go.opentelemetry.io/otel/trace v1.4.1 // indirect
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	go.uber.org/zap v1.21.0 // indirect
	golang.org/x/crypto v0.14.0 // indirect
	golang.org/x/mod v0.11.0 // indirect
	golang.org/x/net v0.17.0 // indirect
	golang.org/x/sys v0.13.0 // indirect
	golang.org/x/text v0.13.0 // indirect
	golang.org/x/tools v0.10.0 // indirect
	google.golang.org/genproto v0.0.0-20230306155012-7f2fa6fef1f4 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/klog/v2 v2.90.1 // indirect
)

// Removes etcd dependency
replace github.com/rexray/gocsi => github.com/dperny/gocsi v1.2.3-pre
