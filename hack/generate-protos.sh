#!/usr/bin/env bash
#
# Regenerates the Go code for every protobuf definition in this repository.
#
# Requires protoc on $PATH; every protoc plugin is built from the versions
# pinned by the tool directives in go.mod.

set -eu -o pipefail

ROOTDIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BINDIR="${ROOTDIR}/bin"

mkdir -p "${BINDIR}"
# -mod=mod because the generators are tool dependencies and so are deliberately
# not vendored.
GOBIN="${BINDIR}" go install -mod=mod \
	google.golang.org/protobuf/cmd/protoc-gen-go \
	google.golang.org/grpc/cmd/protoc-gen-go-grpc \
	github.com/planetscale/vtprotobuf/cmd/protoc-gen-go-vtproto
GOBIN="${BINDIR}" go install "${ROOTDIR}/cmd/protoc-gen-swarm"

# swarmkit's protobuf definitions import each other under their historical
# github.com/docker/swarmkit prefix, which no longer matches the module path.
# Stage a tree that maps that prefix onto the repository root so protoc can
# resolve the imports without requiring a GOPATH checkout. Keeping the prefix
# also keeps the generated descriptors byte-compatible with released versions.
STAGE="$(mktemp -d)"
trap 'rm -rf "${STAGE}"' EXIT
mkdir -p "${STAGE}/github.com/docker"
ln -s "${ROOTDIR}" "${STAGE}/github.com/docker/swarmkit"

PREFIX="github.com/docker/swarmkit"
MODULE="github.com/moby/swarmkit/v2"

# Marshal/unmarshal/size restore the fast codecs that were lost with gogo's
# generated marshallers; clone backs the generated Copy methods.
VTFEATURES="marshal+unmarshal+size+clone+equal"

api_protos=()
for f in "${ROOTDIR}"/api/*.proto; do
	api_protos+=("${PREFIX}/api/$(basename "${f}")")
done

export PATH="${BINDIR}:${PATH}"

# The api package carries services, store objects and raft proxies, so it needs
# every generator.
protoc \
	-I "${STAGE}" -I "${ROOTDIR}/vendor" \
	--go_out="${ROOTDIR}" --go_opt=module="${MODULE}" \
	--go-grpc_out="${ROOTDIR}" --go-grpc_opt=module="${MODULE}" \
	--go-vtproto_out="${ROOTDIR}" --go-vtproto_opt=module="${MODULE}" \
	--go-vtproto_opt=features="${VTFEATURES}" \
	--swarm_out="${ROOTDIR}" --swarm_opt=module="${MODULE}" \
	"${api_protos[@]}"

# api.pb.txt is a published FileDescriptorSet for consumers that generate their
# own bindings. protoc can both emit it and render it as text, so no extra
# tooling is needed.
protoc \
	-I "${STAGE}" -I "${ROOTDIR}/vendor" \
	--descriptor_set_out=/dev/stdout --include_imports \
	"${api_protos[@]}" |
	protoc --decode=google.protobuf.FileDescriptorSet \
		-I "$(dirname "$(command -v protoc)")/../include" \
		google/protobuf/descriptor.proto > "${ROOTDIR}/api/api.pb.txt"

# plugin.proto only declares the custom options consumed by protoc-gen-swarm.
protoc \
	-I "${STAGE}" \
	--go_out="${ROOTDIR}" --go_opt=module="${MODULE}" \
	"${PREFIX}/protobuf/plugin/plugin.proto"

# Test fixtures for the generators themselves.
protoc \
	-I "${STAGE}" \
	--go_out="${ROOTDIR}" --go_opt=module="${MODULE}" \
	--go-vtproto_out="${ROOTDIR}" --go-vtproto_opt=module="${MODULE}" \
	--go-vtproto_opt=features="${VTFEATURES}" \
	--swarm_out="${ROOTDIR}" --swarm_opt=module="${MODULE}" \
	"${PREFIX}/protobuf/plugin/deepcopy/test/deepcopy.proto"

protoc \
	-I "${STAGE}" \
	--go_out="${ROOTDIR}" --go_opt=module="${MODULE}" \
	--go-grpc_out="${ROOTDIR}" --go-grpc_opt=module="${MODULE}" \
	--go-vtproto_out="${ROOTDIR}" --go-vtproto_opt=module="${MODULE}" \
	--go-vtproto_opt=features="${VTFEATURES}" \
	--swarm_out="${ROOTDIR}" --swarm_opt=module="${MODULE}" \
	"${PREFIX}/protobuf/plugin/raftproxy/test/service.proto"
