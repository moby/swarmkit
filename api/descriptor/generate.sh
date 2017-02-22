#!/bin/sh

set -xe

sed 's/package google\.protobuf;/package docker.swarmkit.v1.descriptor;/' ../../vendor/github.com/gogo/protobuf/protobuf/google/protobuf/descriptor.proto > descriptor.proto
protoc -I. --gogoswarm_out=plugins=grpc+deepcopy+raftproxy+authenticatedwrapper,import_path=github.com/docker/swarmkit/api/descriptor:. descriptor.proto
