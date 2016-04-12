//go:generate protoc -I.:../../../..:../../../../../vendor:../../../../../vendor/github.com/gogo/protobuf --gogoswarm_out=plugins=grpc,import_path=github.com/docker/swarm-v2/pb/docker/clusterl/api/manager,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto:. manager.proto

package manager
