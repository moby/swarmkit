//go:generate protoc -I.:../../../../vendor:../../../../vendor/github.com/gogo/protobuf/protobuf --gogoswarm_out=plugins=deepcopy,import_path=github.com/docker/swarmkit/protobuf/plugin/deepcopy/test:. deepcopy.proto

package test
