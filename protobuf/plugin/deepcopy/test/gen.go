//go:generate protoc -I.:../../../../vendor --gogoswarm_out=plugins=deepcopy,import_path=github.com/docker/swarm-v2/protobuf/plugin/deepcopy/test:. deepcopy.proto

package test
