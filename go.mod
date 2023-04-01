module github.com/moby/swarmkit/v2

go 1.18

require (
	code.cloudfoundry.org/clock v1.0.0
	github.com/Microsoft/go-winio v0.5.2
	github.com/akutz/memconn v0.1.0
	github.com/cloudflare/cfssl v0.0.0-20180323000720-5d63dbd981b5
	github.com/container-storage-interface/spec v1.2.0
	github.com/docker/distribution v2.8.1+incompatible
	github.com/docker/docker v23.0.1+incompatible
	github.com/docker/go-connections v0.4.0
	github.com/docker/go-events v0.0.0-20190806004212-e31b211e4f1c
	github.com/docker/go-metrics v0.0.1
	github.com/docker/go-units v0.5.0
	github.com/dustin/go-humanize v1.0.0
	github.com/fernet/fernet-go v0.0.0-20211208181803-9f70042a33ee
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.5.2
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/hashicorp/go-memdb v1.3.2
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.15.0
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.0.2
	github.com/phayes/permbits v0.0.0-20190612203442-39d7c581d2ee
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.14.0
	github.com/rcrowley/go-metrics v0.0.0-20181016184325-3113b8401b8a
	github.com/rexray/gocsi v1.2.2 // replaced; see replace rules for the version used.
	github.com/sirupsen/logrus v1.9.0
	github.com/spf13/cobra v1.6.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.8.1
	go.etcd.io/bbolt v1.3.7
	go.etcd.io/etcd/client/pkg/v3 v3.5.6
	go.etcd.io/etcd/pkg/v3 v3.5.6
	go.etcd.io/etcd/raft/v3 v3.5.6
	go.etcd.io/etcd/server/v3 v3.5.6
	golang.org/x/crypto v0.2.0
	golang.org/x/net v0.5.0
	golang.org/x/time v0.1.0

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
	google.golang.org/grpc v1.48.0
)

require (
	github.com/akutz/gosync v0.1.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/docker/libkv v0.2.2-0.20211217103745-e480589147e3 // indirect
	github.com/fsnotify/fsnotify v1.5.1 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/google/certificate-transparency-go v1.1.4 // indirect
	github.com/hashicorp/go-immutable-radix v1.3.1 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/inconshreveable/mousetrap v1.0.1 // indirect
	github.com/ishidawataru/sctp v0.0.0-20210707070123-9a39160e9062 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/moby/term v0.0.0-20221120202655-abb19827d345 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.3.0 // indirect
	github.com/prometheus/common v0.37.0 // indirect
	github.com/prometheus/procfs v0.8.0 // indirect
	github.com/rootless-containers/rootlesskit v1.1.0 // indirect
	github.com/tedsuo/ifrit v0.0.0-20220120221754-dd274de71113 // indirect
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.8.0 // indirect
	go.uber.org/zap v1.21.0 // indirect
	golang.org/x/sys v0.5.0 // indirect
	golang.org/x/text v0.6.0 // indirect
	google.golang.org/genproto v0.0.0-20220706185917-7780775163c4 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/klog/v2 v2.80.1 // indirect
)

// Removes etcd dependency
replace github.com/rexray/gocsi => github.com/dperny/gocsi v1.2.3-pre
