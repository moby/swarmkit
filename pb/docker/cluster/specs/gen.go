//go:generate protoc -I.:../../..:../../../../vendor/github.com/gogo/protobuf --gogoswarm_out=plugins=grpc+deepcopy,import_path=github.com/docker/swarm-v2/pb/docker/cluster/specs,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mdocker/cluster/types/types.proto=github.com/docker/swarm-v2/pb/docker/cluster/types:. specs.proto

package specs
