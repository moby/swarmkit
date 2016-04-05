//go:generate protoc -I.:../../../vendor:../../../vendor/github.com/gogo/protobuf:../../../../../.. --gogoswarm_out=plugins=grpc,import_path=github.com/docker/swarm-v2/manager/state/pb,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto:. store.proto

package pb
