//go:generate protoc -I.:../../../../:../vendor --gogo_out=plugins=grpc,import_path=github.com/docker/swarm-v2/api:. service.proto types.proto swarm.proto master.proto agent.proto

package api
