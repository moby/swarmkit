//go:generate protoc -I.:../../../../vendor --gogoswarm_out=plugins=grpc+raftproxy,import_path=github.com/docker/swarm-v2/protobuf/plugin/raftproxy/test:. service.proto

package test
