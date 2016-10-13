package deepcopy

import (
	"sort"

	"github.com/docker/swarmkit/protobuf/plugin"
	"github.com/gogo/protobuf/gogoproto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/gogo/protobuf/protoc-gen-gogo/generator"
)

type deepCopyGen struct {
	gen *generator.Generator
}

func init() {
	generator.RegisterPlugin(new(deepCopyGen))
}

func (d *deepCopyGen) Name() string {
	return "deepcopy"
}

func (d *deepCopyGen) Init(g *generator.Generator) {
	d.gen = g
}

func (d *deepCopyGen) genMsgDeepCopy(m *generator.Descriptor) {
	if !plugin.DeepcopyEnabled(m.Options) {
		return
	}

	ccTypeName := generator.CamelCaseSlice(m.TypeName())
	d.gen.P("func (m *", ccTypeName, ") Copy() *", ccTypeName, "{")
	d.gen.P("\tif m == nil {")
	d.gen.P("\t\treturn nil")
	d.gen.P("\t}")
	d.gen.P()

	d.gen.P("\to := &", ccTypeName, "{")

	var funcs []func()
	oneOfFuncs := make(map[string][]func())
	for _, f := range m.Field {
		fName := generator.CamelCase(*f.Name)
		if gogoproto.IsCustomName(f) {
			fName = gogoproto.GetCustomName(f)
		}

		notNullablePrefix := ""
		if !gogoproto.IsNullable(f) {
			notNullablePrefix = "*"

		}

		// Handle all kinds of message type
		if f.IsMessage() {
			// Handle map type
			if mapfunc := d.genMapWriter(m, f, notNullablePrefix); mapfunc != nil {
				funcs = append(funcs, mapfunc)
				continue
			}

			// Handle any message which is not repeated or part of oneof
			if !f.IsRepeated() && f.OneofIndex == nil {
				d.gen.P("\t\t", fName, ": ", notNullablePrefix, "m.", fName, ".Copy(),")
				continue
			}
		}

		// Handle repeated field
		if f.IsRepeated() {
			funcs = append(funcs, d.genRepeatedWriter(m, f, notNullablePrefix))
			continue
		}

		// Handle oneof type
		if f.OneofIndex != nil {
			d.genOneOfWriter(m, f, oneOfFuncs)
			continue
		}

		// Handle all kinds of scalar type
		d.gen.P("\t\t", fName, ": m.", fName, ",")
	}

	d.gen.P("\t}")
	d.gen.P()

	for _, fn := range funcs {
		fn()
	}

	// Sort map keys from oneOfFuncs so that generated code has consistent
	// ordering.
	oneOfKeys := make([]string, 0, len(oneOfFuncs))
	for key := range oneOfFuncs {
		oneOfKeys = append(oneOfKeys, key)
	}
	sort.Strings(oneOfKeys)

	for _, key := range oneOfKeys {
		oneOfNameFuncs := oneOfFuncs[key]
		for _, oneOfFunc := range oneOfNameFuncs {
			oneOfFunc()
		}
		d.gen.P("\t}")
		d.gen.P()
	}

	d.gen.P("\treturn o")
	d.gen.P("}")
	d.gen.P()
}

func (d *deepCopyGen) genMapWriter(m *generator.Descriptor, f *descriptor.FieldDescriptorProto, notNullablePrefix string) func() {
	fName := generator.CamelCase(*f.Name)
	if gogoproto.IsCustomName(f) {
		fName = gogoproto.GetCustomName(f)
	}

	dv := d.gen.ObjectNamed(f.GetTypeName())
	if desc, ok := dv.(*generator.Descriptor); ok && desc.GetOptions().GetMapEntry() {
		mt := d.gen.GoMapType(desc, f)
		typename := mt.GoType
		valueIsMessage := mt.ValueField.IsMessage()
		mapfunc := func() {
			d.gen.P("\tif m.", fName, " != nil {")
			d.gen.P("\t\to.", fName, " = make(", typename, ")")
			d.gen.P("\t\tfor k, v := range m.", fName, " {")
			if valueIsMessage {
				d.gen.P("\t\t\to.", fName, "[k] = ", notNullablePrefix, "v.Copy()")
			} else {
				d.gen.P("\t\t\to.", fName, "[k] = v")
			}
			d.gen.P("\t\t}")
			d.gen.P("\t}")
			d.gen.P()
		}

		return mapfunc
	}

	return nil
}

func (d *deepCopyGen) genRepeatedWriter(m *generator.Descriptor, f *descriptor.FieldDescriptorProto, notNullablePrefix string) func() {
	fName := generator.CamelCase(*f.Name)
	if gogoproto.IsCustomName(f) {
		fName = gogoproto.GetCustomName(f)
	}

	typename, _ := d.gen.GoType(m, f)
	isMessage := f.IsMessage()

	repeatedFunc := func() {
		d.gen.P("\tif m.", fName, " != nil {")
		d.gen.P("\t\to.", fName, " = make(", typename, ", 0, len(m.", fName, "))")
		if isMessage {
			d.gen.P("\t\tfor _, v := range m.", fName, " {")
			d.gen.P("\t\t\to.", fName, " = append(o.", fName, ", ", notNullablePrefix, "v.Copy())")
			d.gen.P("\t\t}")
		} else {
			d.gen.P("\t\to.", fName, " = append(o.", fName, ", m.", fName, "...)")
		}
		d.gen.P("\t}")
		d.gen.P()
	}

	return repeatedFunc
}

func (d *deepCopyGen) genOneOfWriter(m *generator.Descriptor, f *descriptor.FieldDescriptorProto, oneOfFuncs map[string][]func()) {
	ccTypeName := generator.CamelCaseSlice(m.TypeName())
	fName := generator.CamelCase(*f.Name)
	if gogoproto.IsCustomName(f) {
		fName = gogoproto.GetCustomName(f)
	}

	odp := m.OneofDecl[int(*f.OneofIndex)]
	oneOfName := generator.CamelCase(odp.GetName())
	oneOfNameFuncs, ok := oneOfFuncs[oneOfName]
	if !ok {
		oneOfFunc := func() {
			d.gen.P("\tswitch m.", oneOfName, ".(type) {")
		}
		oneOfNameFuncs = append(oneOfNameFuncs, oneOfFunc)
	}

	isMessage := f.IsMessage()
	oneOfFunc := func() {
		tName := ccTypeName + "_" + fName
		d.gen.P("\tcase *", tName, ":")
		d.gen.P("\t\ti := &", tName, " {")
		if isMessage {
			d.gen.P("\t\t\t", fName, ": m.Get", fName, "().Copy(),")
		} else {
			d.gen.P("\t\t\t", fName, ": m.Get", fName, "(),")
		}
		d.gen.P("\t\t}")
		d.gen.P()
		d.gen.P("\t\to.", oneOfName, " = i")
	}

	oneOfNameFuncs = append(oneOfNameFuncs, oneOfFunc)
	oneOfFuncs[oneOfName] = oneOfNameFuncs
}

func (d *deepCopyGen) Generate(file *generator.FileDescriptor) {
	d.gen.P()
	for _, m := range file.Messages() {
		if m.DescriptorProto.GetOptions().GetMapEntry() {
			continue
		}

		d.genMsgDeepCopy(m)
	}
	d.gen.P()
}

func (d *deepCopyGen) GenerateImports(file *generator.FileDescriptor) {
}
