// Command protoc-gen-goswarm is a protoc plugin that generates SwarmKit-specific
// code alongside the standard protoc-gen-go output:
//
//   - *.pb.deepcopy.go   – Copy() and CopyFrom() methods
//   - *.pb.storeobject.go – event types, StoreObject interface methods
//   - *.pb.raftproxy.go  – Raft-leader-aware gRPC server proxy wrappers
//   - *.pb.authwrapper.go – TLS-authorization gRPC server wrappers
//   - rename_map.json    – field/type rename map for proto-name-fix
package main

import (
	"encoding/json"
	"sort"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	pluginpb "github.com/moby/swarmkit/v2/protobuf/plugin"
	"github.com/moby/swarmkit/v2/protobuf/plugin/authenticatedwrapper"
	"github.com/moby/swarmkit/v2/protobuf/plugin/deepcopy"
	"github.com/moby/swarmkit/v2/protobuf/plugin/naming"
	"github.com/moby/swarmkit/v2/protobuf/plugin/raftproxy"
	"github.com/moby/swarmkit/v2/protobuf/plugin/storeobject"
)

func main() {
	protogen.Options{}.Run(func(gen *protogen.Plugin) error {
		// Collect renames across all generated files.
		allRenames := make(map[string]string)

		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			deepcopy.Generate(gen, f)
			storeobject.Generate(gen, f)
			raftproxy.Generate(gen, f)
			authenticatedwrapper.Generate(gen, f)
			collectRenames(f, allRenames)
		}

		if len(allRenames) == 0 {
			return nil
		}

		// Write the rename map to rename_map.json at the output root.
		// proto-name-fix reads this file.
		out := gen.NewGeneratedFile("rename_map.json", "")
		data, err := marshalRenameMap(allRenames)
		if err != nil {
			return err
		}
		out.P(string(data))
		return nil
	})
}

// collectRenames builds the rename map for all messages and enums in f.
func collectRenames(f *protogen.File, renames map[string]string) {
	for _, m := range f.Messages {
		collectMessageRenames(m, renames)
	}
	for _, e := range f.Enums {
		collectEnumRenames("", e, renames)
	}
}

func collectMessageRenames(m *protogen.Message, renames map[string]string) {
	// Field renames (including oneof fields and their wrapper struct types)
	for _, f := range m.Fields {
		std := f.GoName
		final := naming.GoFieldName(f)
		if std != final {
			renames[std] = final
			renames["Get"+std] = "Get" + final
			// For oneof fields, also rename the wrapper struct type:
			// e.g., SelectBy_Id → SelectBy_ID
			if f.Oneof != nil && !f.Oneof.Desc.IsSynthetic() {
				wrapperStd := m.GoIdent.GoName + "_" + std
				wrapperFinal := m.GoIdent.GoName + "_" + final
				renames[wrapperStd] = wrapperFinal
			}
		}
	}
	// Nested enums (use the full Go type name: MessageName_EnumName)
	for _, e := range m.Enums {
		collectEnumRenames(m.GoIdent.GoName, e, renames)
	}
	// Recurse
	for _, nested := range m.Messages {
		collectMessageRenames(nested, renames)
	}
}

// collectEnumRenames handles both top-level and nested enum renames.
// msgPrefix is the enclosing message's Go name (empty for top-level enums).
func collectEnumRenames(msgPrefix string, e *protogen.Enum, renames map[string]string) {
	protoEnumGoName := e.GoIdent.GoName // e.g. "Mount_Type" for nested, "TaskState" for top-level

	desiredTypeName := naming.GoEnumName(e)

	// Only add type rename if the name actually changes
	if protoEnumGoName != desiredTypeName {
		renames[protoEnumGoName] = desiredTypeName
	}

	_ = msgPrefix // used via e.GoIdent.GoName which already includes the prefix

	for _, v := range e.Values {
		std := v.GoIdent.GoName // e.g. "Mount_Type_BIND" or "TaskState_NEW"
		final := naming.GoEnumValueName(v)
		if std != final {
			renames[std] = final
		}
	}
}

// marshalRenameMap produces a canonical JSON representation sorted by key.
func marshalRenameMap(renames map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(renames))
	for k := range renames {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	type kv struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	entries := make([]kv, 0, len(keys))
	for _, k := range keys {
		entries = append(entries, kv{From: k, To: renames[k]})
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return nil, err
	}
	return data, nil
}

// dummy references to ensure imports compile
var _ = proto.HasExtension
var _ = pluginpb.E_GoName
var _ = protoreflect.Name("")
var _ = strings.HasPrefix
