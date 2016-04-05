//go:generate protoc -I.:../vendor:../vendor/github.com/gogo/protobuf --gogoswarm_out=plugins=grpc+deepcopy,import_path=github.com/docker/swarm-v2/api,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto:. types.proto cluster.proto dispatcher.proto

// BUG(stevvooe): The generation line below is nearly identical to the line
// above, except that deepcopy is disabled. There is a bug in deepcopy that
// causes it to use a Copy method on external types not generated with the
// deepcopy plugin. When this bug is resolved, we should be able to generate
// this in one go.

//go:generate protoc -I.:../vendor:../vendor/github.com/gogo/protobuf --gogoswarm_out=plugins=grpc,import_path=github.com/docker/swarm-v2/api,Mgogoproto/gogo.proto=github.com/gogo/protobuf/gogoproto:. manager.proto

package api
