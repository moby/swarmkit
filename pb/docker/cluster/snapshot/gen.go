//go:generate protoc -I.:../../../:../../../../vendor:../../../../vendor/github.com/gogo/protobuf --gogoswarm_out=plugins=grpc,import_path=github.com/docker/swarm-v2/pb/docker/cluster/snapshot,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mdocker/cluster/api/manager/manager.proto=github.com/docker/swarm-v2/pb/docker/cluster/api/manager,Mdocker/cluster/objects/objects.proto=github.com/docker/swarm-v2/pb/docker/cluster/objects:. snapshot.proto

package snapshot
