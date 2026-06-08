// Package naming provides helpers for computing Go identifier names during
// protobuf code generation. It implements the ID-capitalization rules
// (id -> ID, node_id -> NodeID, etc.) plus the
// (docker.protobuf.plugin.go_name) extension support.
package naming

import (
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	pluginpb "github.com/moby/swarmkit/v2/protobuf/plugin"
)

// GoFieldName returns the final Go struct field name for a proto field.
//
// Priority:
//  1. Explicit (docker.protobuf.plugin.go_name) extension → use it verbatim.
//  2. Oneof fields → return f.GoName unchanged (gogo customnameid skipped them).
//  3. Apply ID-capitalization rules (same as customnameid.go).
func GoFieldName(f *protogen.Field) string {
	opts := f.Desc.Options()
	if opts != nil && proto.HasExtension(opts, pluginpb.E_GoName) {
		if v, ok := proto.GetExtension(opts, pluginpb.E_GoName).(string); ok && v != "" {
			return v
		}
		// Legacy *string form
		if v, ok := proto.GetExtension(opts, pluginpb.E_GoName).(*string); ok && v != nil {
			return *v
		}
	}
	if f.Oneof != nil {
		return f.GoName
	}
	return ApplyIDRules(string(f.Desc.Name()), f.GoName)
}

// GoEnumName returns the final Go type name for a proto enum.
func GoEnumName(e *protogen.Enum) string {
	opts := e.Desc.Options()
	if opts != nil && proto.HasExtension(opts, pluginpb.E_GoEnumName) {
		if v, ok := proto.GetExtension(opts, pluginpb.E_GoEnumName).(string); ok && v != "" {
			return v
		}
		if v, ok := proto.GetExtension(opts, pluginpb.E_GoEnumName).(*string); ok && v != nil {
			return *v
		}
	}
	return e.GoIdent.GoName
}

// GoEnumValueName returns the final Go constant name for a proto enum value.
func GoEnumValueName(v *protogen.EnumValue) string {
	opts := v.Desc.Options()
	if opts != nil && proto.HasExtension(opts, pluginpb.E_GoEnumValueName) {
		if s, ok := proto.GetExtension(opts, pluginpb.E_GoEnumValueName).(string); ok && s != "" {
			return s
		}
		if s, ok := proto.GetExtension(opts, pluginpb.E_GoEnumValueName).(*string); ok && s != nil {
			return *s
		}
	}
	return v.GoIdent.GoName
}

// ApplyIDRules applies the ID-capitalisation transformations that were
// previously handled by customnameid.go. protoName is the raw snake_case
// proto field name; goName is the standard CamelCase version produced by
// protoc-gen-go.
//
// Transformations:
//
//	"id"        → "ID"
//	"id_foo"    → "IDFoo"   (prefix rule, e.g. id_prefix → IDPrefix)
//	"foo_id"    → "FooID"   (suffix rule, e.g. node_id   → NodeID)
//	"foo_ids"   → "FooIDs"  (plural suffix, e.g. node_ids → NodeIDs)
func ApplyIDRules(protoName, goName string) string {
	switch {
	case protoName == "id":
		return "ID"
	case strings.HasPrefix(protoName, "id_"):
		// e.g. id_prefix → goName="IdPrefix" → "ID" + "Prefix"
		return "ID" + goName[2:]
	case strings.HasSuffix(protoName, "_id"):
		// e.g. node_id → goName="NodeId" → "Node" + "ID"
		return goName[:len(goName)-2] + "ID"
	case strings.HasSuffix(protoName, "_ids"):
		// e.g. node_ids → goName="NodeIds" → "Node" + "IDs"
		return goName[:len(goName)-3] + "IDs"
	default:
		return goName
	}
}

// RenameMap builds the complete rename map for a set of proto files.
// The keys are the standard CamelCase names produced by protoc-gen-go; the
// values are the desired final names. Only entries where key != value are
// included.
func RenameMap(files []*protogen.File) map[string]string {
	renames := make(map[string]string)
	for _, f := range files {
		if !f.Generate {
			continue
		}
		collectFileRenames(f, renames)
	}
	return renames
}

func collectFileRenames(f *protogen.File, renames map[string]string) {
	for _, m := range f.Messages {
		collectMessageRenames(m, renames)
	}
	for _, e := range f.Enums {
		collectEnumRenames(e, renames)
	}
}

func collectMessageRenames(m *protogen.Message, renames map[string]string) {
	for _, f := range m.Fields {
		std := f.GoName
		final := GoFieldName(f)
		if std != final {
			renames[std] = final
			// Also rename the getter method Get<Name>
			renames["Get"+std] = "Get" + final
		}
	}
	for _, nested := range m.Messages {
		collectMessageRenames(nested, renames)
	}
	for _, e := range m.Enums {
		collectEnumRenames(e, renames)
	}
}

func collectEnumRenames(e *protogen.Enum, renames map[string]string) {
	stdType := e.GoIdent.GoName
	finalType := GoEnumName(e)
	if stdType != finalType {
		renames[stdType] = finalType
	}
	for _, v := range e.Values {
		std := v.GoIdent.GoName
		final := GoEnumValueName(v)
		if std != final {
			renames[std] = final
		}
	}
}

// WatchSelectorField returns the value of the named bool pointer field from a
// WatchSelectors. This helper exists because the WatchSelectors field names
// changed when plugin.pb.go was regenerated (Id instead of ID, etc.).
func WatchSelectorField(ws *pluginpb.WatchSelectors, fieldName protoreflect.Name) *bool {
	m := ws.ProtoReflect()
	fd := m.Descriptor().Fields().ByName(fieldName)
	if fd == nil || !m.Has(fd) {
		return nil
	}
	v := m.Get(fd).Bool()
	return &v
}
