//go:generate protoc -I.:../../../vendor:../../../vendor/github.com/gogo/protobuf --gogoswarm_out=plugins=grpc,import_path=github.com/docker/swarm-v2/state/raft/pb:. raft.proto

package pb
