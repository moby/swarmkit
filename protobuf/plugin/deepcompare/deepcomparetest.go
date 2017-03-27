package deepcompare

import (
	"github.com/gogo/protobuf/gogoproto"
	"github.com/gogo/protobuf/plugin/testgen"
	"github.com/gogo/protobuf/protoc-gen-gogo/generator"
)

type test struct {
	*generator.Generator
}

// NewTest creates a new deepcompare testgen plugin
func NewTest(g *generator.Generator) testgen.TestPlugin {
	return &test{g}
}

func (p *test) Generate(imports generator.PluginImports, file *generator.FileDescriptor) bool {
	used := false
	testingPkg := imports.NewImport("testing")
	randPkg := imports.NewImport("math/rand")
	timePkg := imports.NewImport("time")

	for _, message := range file.Messages() {
		if !gogoproto.HasTestGen(file.FileDescriptorProto, message.DescriptorProto) {
			continue
		}

		if message.DescriptorProto.GetOptions().GetMapEntry() {
			continue
		}

		used = true
		ccTypeName := generator.CamelCaseSlice(message.TypeName())
		p.P()
		p.P(`func Test`, ccTypeName, `Equal(t *`, testingPkg.Use(), `.T) {`)
		p.In()
		p.P(`popr := `, randPkg.Use(), `.New(`, randPkg.Use(), `.NewSource(`, timePkg.Use(), `.Now().UnixNano()))`)
		p.P(`in := NewPopulated`, ccTypeName, `(popr, true)`)
		p.P(`out := in.Copy()`)
		p.P(`if !in.Equal(out) {`)
		p.In()
		p.P(`t.Fatalf("%#v != %#v", in, out)`)
		p.Out()
		p.P(`}`)
		p.P(`out = NewPopulated`, ccTypeName, `(popr, true)`)
		p.P(`if in.Equal(out) {`)
		p.In()
		p.P(`t.Fatalf("%#v == %#v", in, out)`)
		p.Out()
		p.P(`}`)

		// comparing against nil should return false and not panic
		p.P()
		p.P(`in = nil`)
		p.P(`res := in.Equal(out)`)
		p.P(`if res != false {`)
		p.In()
		p.P(`t.Fatalf("comparing against nil (method receiver) should return false, returned: %#v", res)`)
		p.Out()
		p.P(`}`)
		p.P(`res = out.Equal(in)`)
		p.P(`if res != false {`)
		p.In()
		p.P(`t.Fatalf("comparing against nil (method argument) should return false, returned: %#v", res)`)
		p.Out()
		p.P(`}`)

		p.Out()
		p.P(`}`)
	}

	return used
}

func init() {
	testgen.RegisterTestPlugin(NewTest)
}
