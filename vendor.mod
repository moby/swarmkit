// 'vendor.mod' enables use of 'go mod vendor' to managed 'vendor/' directory.
// There is no 'go.mod' file, as that would imply opting in for all the rules
// around SemVer. Switch over may occur in the future, but presently the goal
// is to just use 'go mod vendor' here and other projects including moby.

module github.com/docker/swarmkit

go 1.17

require (
	code.cloudfoundry.org/clock v1.0.0
	github.com/Microsoft/go-winio v0.4.17
	github.com/akutz/memconn v0.1.1-0.20180504174903-e0a19f53d865
	github.com/cloudflare/cfssl v0.0.0-20180323000720-5d63dbd981b5
	github.com/container-storage-interface/spec v1.2.0
	github.com/coreos/etcd v3.3.25+incompatible
	github.com/docker/distribution v2.7.1+incompatible
	github.com/docker/docker v20.10.3-0.20210720110456-471fd2770977+incompatible
	github.com/docker/go-connections v0.4.0
	github.com/docker/go-events v0.0.0-20190806004212-e31b211e4f1c
	github.com/docker/go-metrics v0.0.1
	github.com/docker/go-units v0.4.0
	github.com/dustin/go-humanize v1.0.0
	github.com/fernet/fernet-go v0.0.0-20180830025343-9eac43b88a5e
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.4.3
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/hashicorp/go-memdb v0.0.0-20161216180745-cb9a474f84cc
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.3
	github.com/opencontainers/go-digest v1.0.0
	github.com/opencontainers/image-spec v1.0.1
	github.com/phayes/permbits v0.0.0-20160117212716-f7e3ac5e859d
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.7.1
	github.com/rcrowley/go-metrics v0.0.0-20160113235030-51425a2415d2
	github.com/rexray/gocsi v1.2.1
	github.com/sirupsen/logrus v1.7.0
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.6.1
	go.etcd.io/bbolt v1.3.5
	golang.org/x/crypto v0.0.0-20210322153248-0c34fe9e7dc2
	golang.org/x/net v0.0.0-20210226172049-e18ecbb05110
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e
	google.golang.org/grpc v1.33.2
)

require (
	github.com/akutz/gosync v0.1.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.1 // indirect
	github.com/containerd/containerd v1.5.0-rc.0 // indirect
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/coreos/go-systemd v0.0.0-20190612170431-362f06ec6bc1 // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/docker/libkv v0.2.2-0.20180912205406-458977154600 // indirect
	github.com/google/certificate-transparency-go v1.0.20 // indirect
	github.com/google/uuid v1.2.0 // indirect
	github.com/gorilla/mux v1.8.0 // indirect
	github.com/hashicorp/go-immutable-radix v1.0.0 // indirect
	github.com/hashicorp/golang-lru v0.5.3 // indirect
	github.com/hashicorp/uuid v0.0.0-20160311170451-ebb0a03e909c // indirect
	github.com/hpcloud/tail v1.0.0 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/ishidawataru/sctp v0.0.0-20210226210310-f2269e66cdee // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/moby/term v0.0.0-20210619224110-3f7ff695adc6 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.10.0 // indirect
	github.com/prometheus/procfs v0.6.0 // indirect
	github.com/tedsuo/ifrit v0.0.0-20191009134036-9a97d0632f00 // indirect
	go.uber.org/atomic v1.6.0 // indirect
	go.uber.org/multierr v1.5.0 // indirect
	go.uber.org/zap v1.10.0 // indirect
	golang.org/x/sys v0.0.0-20210823070655-63515b42dcdf // indirect
	golang.org/x/text v0.3.4 // indirect
	google.golang.org/genproto v0.0.0-20201110150050-8816d57aaa9a // indirect
	gopkg.in/fsnotify.v1 v1.4.7 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)

replace (
	github.com/Microsoft/go-winio => github.com/Microsoft/go-winio v0.4.15
	github.com/akutz/gosync => github.com/akutz/gosync v0.1.0
	github.com/containerd/containerd => github.com/containerd/containerd v1.5.2
	github.com/coreos/go-semver => github.com/coreos/go-semver v0.2.0
	github.com/coreos/go-systemd => github.com/coreos/go-systemd v0.0.0-20180511133405-39ca1b05acc7
	github.com/coreos/pkg => github.com/coreos/pkg v0.0.0-20180108230652-97fdf19511ea
	github.com/docker/distribution => github.com/docker/distribution v2.7.1-0.20190205005809-0d3efadf0154+incompatible
	github.com/golang/protobuf => github.com/golang/protobuf v1.3.5
	github.com/google/uuid => github.com/google/uuid v1.0.0
	github.com/hashicorp/go-immutable-radix => github.com/tonistiigi/go-immutable-radix v0.0.0-20170803185627-826af9ccf0fe
	github.com/hpcloud/tail => github.com/hpcloud/tail v1.0.0
	github.com/matttproud/golang_protobuf_extensions => github.com/matttproud/golang_protobuf_extensions v1.0.1
	github.com/onsi/ginkgo => github.com/onsi/ginkgo v1.8.0
	github.com/onsi/gomega => github.com/onsi/gomega v1.5.0
	github.com/opencontainers/runc => github.com/opencontainers/runc v1.0.0-rc95
	github.com/prometheus/client_golang => github.com/prometheus/client_golang v1.6.0
	github.com/prometheus/common => github.com/prometheus/common v0.9.1
	github.com/prometheus/procfs => github.com/prometheus/procfs v0.0.11
	github.com/stretchr/testify => github.com/stretchr/testify v1.3.0
	golang.org/x/crypto => golang.org/x/crypto v0.0.0-20201117144127-c1f2f97bffc9
	golang.org/x/net => golang.org/x/net v0.0.0-20201224014010-6772e930b67b
	golang.org/x/text => golang.org/x/text v0.3.3
	golang.org/x/time => golang.org/x/time v0.0.0-20191024005414-555d28b269f0
	google.golang.org/genproto => google.golang.org/genproto v0.0.0-20200227132054-3f1135a288c9
	google.golang.org/grpc => google.golang.org/grpc v1.23.0
	gopkg.in/fsnotify.v1 => gopkg.in/fsnotify.v1 v1.4.7
)
