# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26
ARG BASE_DEBIAN_DISTRO="bookworm"
ARG GOLANG_IMAGE="golang:${GO_VERSION}-${BASE_DEBIAN_DISTRO}"

ARG PROTOC_VERSION=21.12
ARG PROTOC_GEN_GO_VERSION=v1.36.11
ARG PROTOC_GEN_GO_GRPC_VERSION=v1.6.2
ARG GOLANGCI_LINT_VERSION=v2.12.2

# gobase
FROM --platform=$BUILDPLATFORM ${GOLANG_IMAGE} AS gobase
RUN apt-get update && apt-get install -y --no-install-recommends git make rsync
WORKDIR /go/src/github.com/docker/swarmkit
RUN git config --global --add safe.directory /go/src/github.com/docker/swarmkit

FROM gobase AS packages
RUN --mount=target=. \
  mkdir -p /tmp/packages && \
  echo $(go list ./...) | tee /tmp/packages/packages; \
  echo $(go list ./integration) | tee /tmp/packages/integration-packages;

FROM gobase AS vendored
RUN --mount=target=.,rw \
    --mount=target=/go/pkg/mod,type=cache <<EOT
  set -e
  make go-mod-vendor
  mkdir /out
  cp -r go.mod go.sum vendor /out
EOT

FROM scratch AS vendor-update
COPY --from=vendored /out /

FROM gobase AS vendor-validate
RUN --mount=type=bind,target=.,rw \
    --mount=from=vendored,source=/out,target=/out <<EOT
  set -e
  git add -A
  rm -rf vendor
  cp -rf /out/* .
  if [ -n "$(git status --porcelain -- go.mod go.sum vendor)" ]; then
    echo >&2 'ERROR: Vendor result differs. Please vendor your package with "make go-mod-vendor"'
    git status --porcelain -- go.mod go.sum vendor
    exit 1
  fi
EOT

FROM gobase AS generate-base
RUN apt-get --no-install-recommends install -y unzip
ARG PROTOC_VERSION
ARG TARGETOS
ARG TARGETARCH
RUN <<EOT
  set -e
  arch=$(echo $TARGETARCH | sed -e s/amd64/x86_64/ -e s/arm64/aarch_64/)
  wget -q https://github.com/protocolbuffers/protobuf/releases/download/v${PROTOC_VERSION}/protoc-${PROTOC_VERSION}-${TARGETOS}-${arch}.zip
  unzip protoc-${PROTOC_VERSION}-${TARGETOS}-${arch}.zip -d /usr/local
EOT
# Install the standard protobuf Go plugins (protoc-gen-goswarm and
# proto-name-fix are built from this repo by `make protos`).
ARG PROTOC_GEN_GO_VERSION
ARG PROTOC_GEN_GO_GRPC_VERSION
RUN --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/go/pkg/mod <<EOT
  set -e
  go install google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION}
  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@${PROTOC_GEN_GO_GRPC_VERSION}
EOT

FROM generate-base AS generate-build
RUN --mount=type=bind,target=.,rw \
    --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/go/pkg/mod <<EOT
  set -ex
  make protos
  mkdir /out
  git ls-files -m --others -- ':!vendor' '**/*.pb.go' '**/*.pb.*.go' | tar -cf - --files-from - | tar -C /out -xf -
EOT

FROM scratch AS generate-update
COPY --from=generate-build /out /

FROM gobase AS generate-validate
RUN --mount=type=bind,target=.,rw \
    --mount=type=bind,from=generate-build,source=/out,target=/generated <<EOT
  set -e
  git add -A
  if [ "$(ls -A /generated)" ]; then
    cp -rf /generated/* .
  fi
  diff=$(git status --porcelain -- ':!vendor' '**/*.pb.go' '**/*.pb.*.go')
  if [ -n "$diff" ]; then
    echo >&2 'ERROR: The result of "go generate" differs. Please update with "make generate"'
    echo "$diff"
    exit 1
  fi
EOT

FROM golangci/golangci-lint:${GOLANGCI_LINT_VERSION} AS golangci-lint
FROM gobase AS lint
RUN apt-get install -y --no-install-recommends libgcc-12-dev libc6-dev
RUN --mount=type=bind,target=. \
    --mount=type=cache,target=/root/.cache \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=from=golangci-lint,source=/usr/bin/golangci-lint,target=/usr/bin/golangci-lint <<EOT
  set -e
  config=$(pwd)/.golangci.yml
  for dir in . swarmd; do
    (
      set -x
      cd $dir
      golangci-lint run --config "$config" ./...
    )
  done
EOT

FROM gobase AS fmt-proto
RUN --mount=type=bind,target=. \
    make fmt-proto

# use generate-base to have protoc available
FROM generate-base
ENV GO111MODULE=on
# install the dependencies from `make setup`
# we only copy `direct.mk` to avoid busting the cache too easily
COPY direct.mk .
COPY go.* .
RUN make --file=direct.mk setup
# now we can copy the rest
COPY . .
# default to just `make`. If you want to change the default command, change the
# default make command, not this command.
CMD ["make"]
