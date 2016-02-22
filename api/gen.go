//go:generate protoc -I.:../../../../:../vendor --gogoswarm_out=plugins=grpc,import_path=github.com/docker/swarm-v2/api:. types.proto swarm.proto master.proto agent.proto

package api
