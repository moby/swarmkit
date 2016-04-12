//go:generate protoc -I.:../../..:../../../../vendor/github.com/gogo/protobuf --gogoswarm_out=plugins=grpc+deepcopy,import_path=github.com/docker/swarm-v2/pb/docker/cluster/objects,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mdocker/cluster/types/types.proto=github.com/docker/swarm-v2/pb/docker/cluster/types,Mdocker/cluster/specs/specs.proto=github.com/docker/swarm-v2/pb/docker/cluster/specs:. objects.proto

package objects
