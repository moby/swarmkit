//go:generate protoc -I.:../vendor --gogoswarm_out=plugins=grpc,import_path=github.com/docker/swarm-v2/api:. types.proto cluster.proto dispatcher.proto

//go:generate protoc -I.:../vendor:../vendor/github.com/gogo/protobuf --gogoswarm_out=plugins=grpc,import_path=github.com/docker/swarm-v2/api:. manager.proto

package api
