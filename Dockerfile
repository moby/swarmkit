# syntax=docker/dockerfile:1

ARG GO_VERSION=1.24
ARG PROTOC_VERSION=3.11.4
ARG GOLANGCI_LINT_VERSION=v1.50.1
ARG DEBIAN_FRONTEND=noninteractive

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bullseye AS gobase
ARG DEBIAN_FRONTEND
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

FROM gobase AS protoc-gen-gogoswarm
RUN --mount=type=bind,target=.,rw \
    --mount=type=cache,target=/root/.cache \
    make bin/protoc-gen-gogoswarm && mv bin/protoc-gen-gogoswarm /usr/local/bin/

FROM gobase AS protobuild
RUN --mount=type=bind,source=tools,target=. \
    --mount=target=/go/pkg/mod,type=cache \
    --mount=type=cache,target=/root/.cache \
    go install -mod=mod github.com/containerd/protobuild

FROM gobase AS generate-base
ARG DEBIAN_FRONTEND
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

FROM generate-base AS generate-build
RUN --mount=type=bind,target=.,rw \
    --mount=from=packages,source=/tmp/packages,target=/tmp/packages \
    --mount=from=protobuild,source=/go/bin/protobuild,target=/usr/bin/protobuild \
    --mount=from=protoc-gen-gogoswarm,source=/usr/local/bin/protoc-gen-gogoswarm,target=/usr/bin/protoc-gen-gogoswarm <<EOT
  set -ex
  protobuild $(cat /tmp/packages/packages)
  go generate -mod=vendor -x $(cat /tmp/packages/packages)
  mkdir /out
  git ls-files -m --others -- ':!vendor' '**/*.pb.go' | tar -cf - --files-from - | tar -C /out -xf -
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
  diff=$(git status --porcelain -- ':!vendor' '**/*.pb.go')
  if [ -n "$diff" ]; then
    echo >&2 'ERROR: The result of "go generate" differs. Please update with "make generate"'
    echo "$diff"
    exit 1
  fi
EOT

FROM golangci/golangci-lint:${GOLANGCI_LINT_VERSION} AS golangci-lint
FROM gobase AS lint
ARG DEBIAN_FRONTEND
RUN apt-get install -y --no-install-recommends libgcc-10-dev libc6-dev
RUN --mount=type=bind,target=. \
    --mount=type=cache,target=/root/.cache \
    --mount=from=golangci-lint,source=/usr/bin/golangci-lint,target=/usr/bin/golangci-lint \
    golangci-lint run ./... && cd swarmd && golangci-lint run ./...

FROM gobase AS fmt-proto
RUN --mount=type=bind,target=. \
    make fmt-proto

# use generate-base to have protoc available
FROM generate-base
ENV GO111MODULE=on
# install the dependencies from `make setup`
# we only copy `direct.mk` to avoid busting the cache too easily
COPY direct.mk .
COPY tools .
RUN make --file=direct.mk setup
# now we can copy the rest
COPY . .
# default to just `make`. If you want to change the default command, change the
# default make command, not this command.
CMD ["make"]
