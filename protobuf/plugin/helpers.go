package plugin

import (
	"google.golang.org/protobuf/proto"
	descriptorpb "google.golang.org/protobuf/types/descriptorpb"
)

// DeepcopyEnabled returns true if deepcopy is enabled for the descriptor.
func DeepcopyEnabled(options *descriptorpb.MessageOptions) bool {
	if options == nil {
		return true
	}
	if !proto.HasExtension(options, E_Deepcopy) {
		return true // default is true
	}
	v := proto.GetExtension(options, E_Deepcopy)
	// Standard proto API returns bool (not *bool)
	if b, ok := v.(bool); ok {
		return b
	}
	if b, ok := v.(*bool); ok && b != nil {
		return *b
	}
	return true
}
