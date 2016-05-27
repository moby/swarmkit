package api

//go:generate protoc -I.:../protobuf:../vendor:../vendor/github.com/gogo/protobuf --gogoswarm_out=plugins=grpc+deepcopy+raftproxy+authenticatedwrapper,import_path=github.com/docker/swarm-v2/api,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto,Mtimestamp/timestamp.proto=github.com/docker/swarm-v2/api/timestamp,Mgoogle/protobuf/descriptor.proto=github.com/gogo/protobuf/protoc-gen-gogo/descriptor:. types.proto specs.proto objects.proto control.proto dispatcher.proto ca.proto snapshot.proto raft.proto auth.proto
