package plugin

import (
	"github.com/gogo/protobuf/proto"
	google_protobuf "github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
)

func DeepcopyEnabled(f *google_protobuf.FieldDescriptorProto) bool {
	return proto.GetBoolExtension(f.Options, E_Deepcopy, true)
}
