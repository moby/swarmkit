package protobuf

import (
	"strings"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/descriptor"
	"github.com/pkg/errors"
)

func (indexer *Indexer) compile(descriptors *descriptor.FileDescriptorSet, typePath string, indices []*api.IndexDeclaration) error {
	for _, index := range indices {
		protobufIndex, err := resolve(descriptors, typePath, index.Field)
		if err != nil {
			return err
		}
		protobufIndex.key = index.Key
		indexer.indices = append(indexer.indices, protobufIndex)
	}
	return nil
}

func resolve(descriptors *descriptor.FileDescriptorSet, msgPath, fieldPath string) (protobufIndex, error) {
	splitMsgPath := strings.Split(msgPath, ".")
	if len(splitMsgPath) < 2 {
		return protobufIndex{}, errors.New(`type path does not contain any "." separators`)
	}
	splitFieldPath := strings.Split(fieldPath, ".")

	// package can be a variable number of components
	for i := 1; i != len(splitMsgPath); i++ {
		packageName := strings.Join(splitMsgPath[:len(splitMsgPath)-i], ".")
		typePath := splitMsgPath[len(splitMsgPath)-i:]
		msg := resolveMessage(descriptors, packageName, typePath)
		if msg == nil {
			continue
		}

		// Found message; follow fields
		resolvedIndex := resolveFields(descriptors, msg, packageName, typePath, splitFieldPath)
		if resolvedIndex == nil {
			return protobufIndex{}, errors.Errorf("could not resolve field %s", fieldPath)
		}
		return *resolvedIndex, nil
	}

	return protobufIndex{}, errors.Errorf("could not resolve message %s", msgPath)
}

func resolveMessage(descriptors *descriptor.FileDescriptorSet, packageName string, typePath []string) *descriptor.DescriptorProto {
	for _, file := range descriptors.GetFile() {
		if strings.Map(dotToUnderscore, file.GetPackage()) != strings.Map(dotToUnderscore, packageName) {
			continue
		}

		var msg *descriptor.DescriptorProto

		for _, msg = range file.GetMessageType() {
			if msg.GetName() != typePath[0] {
				continue
			}
			for i := 1; i != len(typePath); i++ {
				nestedTypes := msg.GetNestedType()
				msg = nil

				for _, nested := range nestedTypes {
					if nested.GetName() == typePath[i] {
						msg = nested
						break
					}
				}

				if msg == nil {
					return nil
				}
			}
			return msg
		}
	}

	return nil
}

func dotToUnderscore(r rune) rune {
	if r == '.' {
		return '_'
	}
	return r
}

func resolveFields(descriptors *descriptor.FileDescriptorSet, msg *descriptor.DescriptorProto, packageName string, typePath, fieldPath []string) *protobufIndex {
	protobufIndex := &protobufIndex{}

	var (
		fieldType         descriptor.FieldDescriptorProto_Type
		fieldLabel        descriptor.FieldDescriptorProto_Label
		fieldDefaultValue string
	)

	for _, fieldComponent := range fieldPath {
		if msg == nil {
			return nil
		}

		field := getField(msg, fieldComponent)
		if field == nil {
			return nil
		}

		fieldNum := field.GetNumber()
		fieldType = field.GetType()
		fieldLabel = field.GetLabel()
		fieldDefaultValue = field.GetDefaultValue()
		if fieldType == descriptor.FieldDescriptorProto_TYPE_MESSAGE {
			msg, packageName, typePath = getNextMessage(descriptors, packageName, typePath, field.GetTypeName())
		} else {
			msg = nil
		}
		protobufIndex.fieldNumberPath = append(protobufIndex.fieldNumberPath, fieldNum)
	}

	protobufIndex.finalType = fieldType
	protobufIndex.finalLabel = fieldLabel
	protobufIndex.defaultValue = fieldDefaultValue
	if protobufIndex.defaultValue == "" {
		if isNumericType(protobufIndex.finalType) {
			protobufIndex.defaultValue = "0"
		} else if protobufIndex.finalType == descriptor.FieldDescriptorProto_TYPE_BOOL {
			protobufIndex.defaultValue = "false"
		}
	}
	return protobufIndex
}

func getField(msg *descriptor.DescriptorProto, fieldName string) *descriptor.FieldDescriptorProto {
	for _, field := range msg.GetField() {
		if field.GetName() == fieldName {
			return field
		}
	}

	return nil
}

func getNextMessage(descriptors *descriptor.FileDescriptorSet, packageName string, typePath []string, typeName string) (*descriptor.DescriptorProto, string, []string) {
	if len(typeName) == 0 {
		return nil, "", nil
	}

	if typeName[0] != '.' {
		extraComponents := strings.Split(typeName, ".")
		prospectiveTypePath := make([]string, len(typePath)+len(extraComponents))
		copy(prospectiveTypePath, typePath)

		for i := len(typePath); i >= 0; i-- {
			prospectiveTypePath = prospectiveTypePath[:i]
			prospectiveTypePath = append(prospectiveTypePath, extraComponents...)
			msg := resolveMessage(descriptors, packageName, prospectiveTypePath)
			if msg != nil {
				return msg, packageName, prospectiveTypePath
			}
		}
	} else {
		typeName = typeName[1:]
	}

	// Try treating this as a full path, with a package name
	fullPath := strings.Split(typeName, ".")

	if len(fullPath) < 2 {
		return nil, "", nil
	}

	// package can be a variable number of components
	for i := 1; i != len(fullPath); i++ {
		// type can be a variable number of components
		prospectivePackageName := strings.Join(fullPath[:len(fullPath)-i], ".")
		prospectiveTypePath := fullPath[len(fullPath)-i:]
		msg := resolveMessage(descriptors, prospectivePackageName, prospectiveTypePath)
		if msg != nil {
			return msg, prospectivePackageName, prospectiveTypePath
		}
	}

	return nil, "", nil
}

func isNumericType(fieldType descriptor.FieldDescriptorProto_Type) bool {
	switch fieldType {
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE,
		descriptor.FieldDescriptorProto_TYPE_FLOAT,
		descriptor.FieldDescriptorProto_TYPE_INT64,
		descriptor.FieldDescriptorProto_TYPE_UINT64,
		descriptor.FieldDescriptorProto_TYPE_INT32,
		descriptor.FieldDescriptorProto_TYPE_FIXED64,
		descriptor.FieldDescriptorProto_TYPE_FIXED32,
		descriptor.FieldDescriptorProto_TYPE_UINT32,
		descriptor.FieldDescriptorProto_TYPE_ENUM,
		descriptor.FieldDescriptorProto_TYPE_SFIXED32,
		descriptor.FieldDescriptorProto_TYPE_SFIXED64,
		descriptor.FieldDescriptorProto_TYPE_SINT32,
		descriptor.FieldDescriptorProto_TYPE_SINT64:
		return true
	default:
		return false
	}
}
