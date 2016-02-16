//go:generate protoc -I.:../../../../:../vendor --gogo_out=plugins=grpc,import_path=github.com/docker/swarm-v2/api:. types.proto api.proto

package api
