package deepcompare

import (
	"github.com/docker/swarmkit/protobuf/plugin"
	"github.com/gogo/protobuf/gogoproto"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/gogo/protobuf/protoc-gen-gogo/generator"
)

type deepCompareGen struct {
	*generator.Generator
	generator.PluginImports
	deepcomparePkg generator.Single
	bytesPkg       generator.Single
}

func init() {
	generator.RegisterPlugin(new(deepCompareGen))
}

func (d *deepCompareGen) Name() string {
	return "deepcompare"
}

func (d *deepCompareGen) Init(g *generator.Generator) {
	d.Generator = g
}

func (d *deepCompareGen) genEqualFunc(msg1, msg2 string) {
	d.P("if !", d.deepcomparePkg.Use(), ".Equal(", msg1, ", ", msg2, ") {")
	d.In()
	d.P("return false")
	d.Out()
	d.P("}")
}

func (d *deepCompareGen) genScalarEqual(var1, var2 string) {
	d.P("if ", var1, " != ", var2, " {")
	d.In()
	d.P("return false")
	d.Out()
	d.P("}")
}

func (d *deepCompareGen) genBytesEqual(var1, var2 string) {
	d.P("if !", d.bytesPkg.Use(), ".Equal(", var1, ", ", var2, ") {")
	d.In()
	d.P("return false")
	d.Out()
	d.P("}")
}

func (d *deepCompareGen) genMsgDeepEqual(m *generator.Descriptor) {
	ccTypeName := generator.CamelCaseSlice(m.TypeName())

	d.P("func (m *", ccTypeName, ") Equal(other interface{})", " bool {")
	d.In()
	d.P("o := other.(*", ccTypeName, ")")
	d.P("if m == nil || o == nil {")
	d.In()
	d.P("return m == o")
	d.Out()
	d.P("}")

	oneofByIndex := [][]*descriptor.FieldDescriptorProto{}
	for _, f := range m.Field {
		fName := generator.CamelCase(*f.Name)
		if gogoproto.IsCustomName(f) {
			fName = gogoproto.GetCustomName(f)
		}

		// Handle oneof type, we defer them to a loop below
		if f.OneofIndex != nil {
			if len(oneofByIndex) <= int(*f.OneofIndex) {
				oneofByIndex = append(oneofByIndex, []*descriptor.FieldDescriptorProto{})
			}

			oneofByIndex[*f.OneofIndex] = append(oneofByIndex[*f.OneofIndex], f)
			continue
		}

		// Handle all kinds of message type
		if f.IsMessage() {
			// Handle map type
			if d.genMap(m, f) {
				continue
			}

			// Handle any message which is not repeated or part of oneof
			if !f.IsRepeated() && f.OneofIndex == nil {
				if !gogoproto.IsNullable(f) {
					d.genEqualFunc("&m."+fName, "&o."+fName)
				} else {
					d.genEqualFunc("m."+fName, "o."+fName)
				}
				continue
			}
		}

		// Handle repeated field
		if f.IsRepeated() {
			d.genRepeated(m, f)
			continue
		}

		// Handle bytes
		if f.IsBytes() {
			d.genBytesEqual("m."+fName, "o."+fName)
			continue
		}

		// scalar: normal equality
		d.genScalarEqual("m."+fName, "o."+fName)
	}

	for i, oo := range m.GetOneofDecl() {
		d.genOneOf(m, oo, oneofByIndex[i])
	}

	d.P("return true")
	d.Out()
	d.P("}")
	d.P()
}

func (d *deepCompareGen) genMap(m *generator.Descriptor, f *descriptor.FieldDescriptorProto) bool {
	fName := generator.CamelCase(*f.Name)
	if gogoproto.IsCustomName(f) {
		fName = gogoproto.GetCustomName(f)
	}

	dv := d.ObjectNamed(f.GetTypeName())
	desc, ok := dv.(*generator.Descriptor)
	if !ok || !desc.GetOptions().GetMapEntry() {
		return false
	}

	mt := d.GoMapType(desc, f)

	d.genScalarEqual("len(m."+fName+")", "len(o."+fName+")")
	d.P("for k, vo := range o.", fName, " {")
	d.In()
	d.P("vm, ok := m.", fName, "[k]")
	d.P("if !ok {")
	d.In()
	d.P("return false")
	d.Out()
	d.P("}")
	if mt.ValueField.IsMessage() {
		if !gogoproto.IsNullable(f) {
			d.genEqualFunc("&vo", "&vm")
		} else {
			d.genEqualFunc("vo", "vm")
		}
	} else if mt.ValueField.IsBytes() {
		d.genBytesEqual("vo", "vm")
	} else {
		d.genScalarEqual("vo", "vm")
	}
	d.Out()
	d.P("}")
	d.P()

	return true
}

func (d *deepCompareGen) genRepeated(m *generator.Descriptor, f *descriptor.FieldDescriptorProto) {
	fName := generator.CamelCase(*f.Name)
	if gogoproto.IsCustomName(f) {
		fName = gogoproto.GetCustomName(f)
	}

	d.genScalarEqual("len(m."+fName+")", "len(o."+fName+")")

	if proto.GetBoolExtension(f.Options, plugin.E_OrderInsensitive, false) {
		// Order insensitive compare
		d.P("remaining", fName, " := make(map[int]struct{})")
		d.P("for i := range o.", fName, " {")
		d.In()
		d.P("remaining", fName, "[i] = struct{}{}")
		d.Out()
		d.P("}")
		d.P("outer", fName, " :")
		d.P("for i := range m.", fName, " {")
		d.In()
		d.P("for j := range remaining", fName, " {")
		d.In()
		if f.IsMessage() {
			if !gogoproto.IsNullable(f) {
				d.P("if ", d.deepcomparePkg.Use(), ".Equal(&m.", fName, "[i], &o.", fName, "[j]) {")
			} else {
				d.P("if ", d.deepcomparePkg.Use(), ".Equal(m.", fName, "[i], o.", fName, "[j]) {")
			}
		} else if f.IsBytes() {
			d.P("if ", d.bytesPkg.Use(), ".Equal(m.", fName, "[i], o.", fName, "[j]) {")
		} else {
			d.P("if m.", fName, "[i] != o.", fName, "[j] {")
		}
		d.In()
		d.P("delete(remaining", fName, ", j)")
		d.P("continue outer", fName)
		d.Out()
		d.P("}")
		d.Out()
		d.P("}")
		d.P("return false")
		d.Out()
		d.P("}")
	} else {

		d.P("for i := range m.", fName, " {")
		d.In()
		if f.IsMessage() {
			if !gogoproto.IsNullable(f) {
				d.genEqualFunc("&m."+fName+"[i]", "&o."+fName+"[i]")
			} else {
				d.genEqualFunc("m."+fName+"[i]", "o."+fName+"[i]")
			}
		} else if f.IsBytes() {
			d.genBytesEqual("m."+fName+"[i]", "o."+fName+"[i]")
		} else {
			d.genScalarEqual("m."+fName+"[i]", "o."+fName+"[i]")
		}
		d.Out()
		d.P("}")
	}
	d.P()
}

func (d *deepCompareGen) genOneOf(m *generator.Descriptor, oneof *descriptor.OneofDescriptorProto, fields []*descriptor.FieldDescriptorProto) {
	oneOfName := generator.CamelCase(oneof.GetName())

	d.P("switch x := o.", oneOfName, ".(type) {")

	for _, f := range fields {
		ccTypeName := generator.CamelCaseSlice(m.TypeName())
		fName := generator.CamelCase(*f.Name)
		if gogoproto.IsCustomName(f) {
			fName = gogoproto.GetCustomName(f)
		}

		tName := ccTypeName + "_" + fName
		d.P("case *", tName, ":")
		d.In()

		d.P("y, ok := m.", oneOfName, ".(*", tName, ")")
		d.P("if !ok {")
		d.In()
		d.P("return false")
		d.Out()
		d.P("}")

		if f.IsMessage() {
			d.genEqualFunc("x."+fName, "y."+fName)
		} else if f.IsBytes() {
			d.genBytesEqual("x."+fName, "y."+fName)
		} else {
			d.genScalarEqual("x."+fName, "y."+fName)
		}

		d.Out()
	}

	d.Out()
	d.P("}")
	d.P()
}

func (d *deepCompareGen) Generate(file *generator.FileDescriptor) {
	d.PluginImports = generator.NewPluginImports(d.Generator)

	// TODO(stevvooe): Ideally, this could be taken as a parameter to the
	// deepcompare plugin to control the package import, but this is good enough,
	// for now.
	d.deepcomparePkg = d.NewImport("github.com/docker/swarmkit/api/deepcompare")

	d.bytesPkg = d.NewImport("bytes")

	d.P()
	for _, m := range file.Messages() {
		if m.DescriptorProto.GetOptions().GetMapEntry() {
			continue
		}

		if !plugin.DeepcompareEnabled(m.Options) {
			continue
		}

		d.genMsgDeepEqual(m)
	}
	d.P()
}
