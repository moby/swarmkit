//go:generate protoc -I.:../../../../vendor:../../../../vendor/github.com/gogo/protobuf/protobuf --gogoswarm_out=plugins=grpc+raftproxy,import_path=github.com/docker/swarmkit/protobuf/plugin/raftproxy/test:. service.proto

package test
