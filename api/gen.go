//go:generate protoc -I.:../vendor --gogoswarm_out=plugins=grpc+deepcopy,import_path=github.com/docker/swarm-v2/api:. types.proto cluster.proto dispatcher.proto manager.proto

//go:generate protoc -I.:../vendor:../vendor/github.com/gogo/protobuf --gogoswarm_out=plugins=grpc,import_path=github.com/docker/swarm-v2/api:. raft.proto

package api
