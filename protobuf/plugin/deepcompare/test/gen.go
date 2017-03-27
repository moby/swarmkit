//go:generate protoc -I.:../../../../vendor --gogoswarm_out=plugins=deepcompare+deepcopy,import_path=github.com/docker/swarmkit/protobuf/plugin/deepcompare/test:. deepcompare.proto

package test
