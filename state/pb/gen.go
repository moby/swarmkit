//go:generate protoc -I.:../../vendor:../../../../.. --gogoswarm_out=plugins=grpc,import_path=github.com/docker/swarm-v2/state/pb:. store.proto

package pb
