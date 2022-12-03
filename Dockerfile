# syntax=docker/dockerfile:1

ARG GO_VERSION=1.18.9
ARG PROTOC_VERSION=3.11.4
ARG DEBIAN_FRONTEND=noninteractive

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bullseye AS gobase
ARG DEBIAN_FRONTEND
RUN apt-get update && apt-get install -y --no-install-recommends git make rsync
WORKDIR /go/src/github.com/docker/swarmkit

FROM gobase AS packages
RUN --mount=target=. \
  mkdir -p /tmp/packages && \
  echo $(go list ./...) | tee /tmp/packages/packages; \
  echo $(go list ./integration) | tee /tmp/packages/integration-packages;

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
