module github.com/moby/moby/tools

go 1.17

require github.com/containerd/protobuild v0.1.1-0.20211025221430-7e5ee24bc1f7

require (
	github.com/golang/protobuf v1.5.0 // indirect
	github.com/pelletier/go-toml v1.8.1 // indirect
)

// match ../vendor.mod
replace github.com/golang/protobuf => github.com/golang/protobuf v1.3.5
