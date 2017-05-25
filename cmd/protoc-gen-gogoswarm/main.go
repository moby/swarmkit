package main

import (
	"path/filepath"

	_ "github.com/docker/swarmkit/protobuf/plugin/authenticatedwrapper"
	_ "github.com/docker/swarmkit/protobuf/plugin/deepcopy"
	_ "github.com/docker/swarmkit/protobuf/plugin/raftproxy"
	_ "github.com/docker/swarmkit/protobuf/plugin/storeobject"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/gogo/protobuf/vanity"
	"github.com/gogo/protobuf/vanity/command"
)

func notGoogleProtobufDescriptorProto(file *descriptor.FileDescriptorProto) bool {
	// can not just check if file.GetName() == "google/protobuf/descriptor.proto" because we do not want to assume compile path
	_, fileName := filepath.Split(file.GetName())
	return !((file.GetPackage() == "google.protobuf" || file.GetPackage() == "docker.swarmkit.v1.descriptor") && fileName == "descriptor.proto")
}

func main() {
	req := command.Read()
	files := req.GetProtoFile()

	for _, opt := range []func(*descriptor.FileDescriptorProto){
		vanity.TurnOnMarshalerAll,
		vanity.TurnOnSizerAll,
		vanity.TurnOnUnmarshalerAll,
	} {
		vanity.ForEachFile(files, opt)
	}

	files = vanity.FilterFiles(files, notGoogleProtobufDescriptorProto)

	for _, opt := range []func(*descriptor.FileDescriptorProto){
		vanity.TurnOffGoGettersAll,
		vanity.TurnOffGoStringerAll,
		vanity.TurnOnStringerAll,
		CustomNameID,
	} {
		vanity.ForEachFile(files, opt)
	}

	resp := command.Generate(req)
	command.Write(resp)
}
