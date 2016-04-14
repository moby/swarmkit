//go:generate protoc -I.:../../vendor:../../vendor/github.com/gogo/protobuf --gogoswarm_out=plugins=grpc+deepcopy+raftproxy,import_path=github.com/docker/swarm-v2/api/timestamp,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto:. timestamp.proto

package timestamp
