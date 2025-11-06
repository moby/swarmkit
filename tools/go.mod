module github.com/moby/swarmkit/v2/tools

go 1.21.0

require github.com/containerd/protobuild v0.1.1-0.20211025221430-7e5ee24bc1f7

require (
	github.com/golang/protobuf v1.5.0 // indirect
	github.com/pelletier/go-toml v1.8.1 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
)

// match ../go.mod
replace github.com/golang/protobuf => github.com/golang/protobuf v1.5.3
