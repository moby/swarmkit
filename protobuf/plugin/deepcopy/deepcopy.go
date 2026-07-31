package deepcopy

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/moby/swarmkit/v2/protobuf/plugin"
)

// Generate emits the Copy and CopyFrom methods for every message that has
// deepcopy enabled.
//
// The copying itself is done by protoc-gen-go-vtproto's CloneVT and by
// proto.Merge; these methods only exist to keep the spelling swarmkit has
// always used, and to keep the deepcopy option meaningful.
func Generate(g *protogen.GeneratedFile, f *protogen.File) {
	for _, m := range f.Messages {
		genCopy(g, m)
	}
}

func genCopy(g *protogen.GeneratedFile, m *protogen.Message) {
	for _, nested := range m.Messages {
		genCopy(g, nested)
	}

	// Map entries are synthesized messages with no Go type of their own.
	if m.Desc.IsMapEntry() {
		return
	}

	opts, _ := m.Desc.Options().(*descriptorpb.MessageOptions)
	if !plugin.DeepcopyEnabled(opts) {
		return
	}

	name := m.GoIdent.GoName
	g.P("func (m *", name, ") Copy() *", name, " {")
	g.P("return m.CloneVT()")
	g.P("}")
	g.P()

	protoPkg := protogen.GoImportPath("google.golang.org/protobuf/proto")
	g.P("func (m *", name, ") CopyFrom(src any) {")
	g.P(g.QualifiedGoIdent(protoPkg.Ident("Reset")), "(m)")
	g.P(g.QualifiedGoIdent(protoPkg.Ident("Merge")), "(m, src.(*", name, "))")
	g.P("}")
	g.P()
}
