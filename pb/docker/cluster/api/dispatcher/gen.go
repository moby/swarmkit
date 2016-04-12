//go:generate protoc -I.:../../../..:../../../../../vendor/github.com/gogo/protobuf --gogoswarm_out=plugins=grpc+deepcopy,import_path=github.com/docker/swarm-v2/pb/docoker/cluster/api/dispatcher,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mdocker/cluster/types/types.proto=github.com/docker/swarm-v2/pb/docker/cluster/types,Mdocker/cluster/objects/objects.proto=github.com/docker/swarm-v2/pb/docker/cluster/objects:. dispatcher.proto

package dispatcher
