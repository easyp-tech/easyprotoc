package options

import (
	"strings"

	"github.com/easyp-tech/easyprotoc/internal/linker"
	"github.com/easyp-tech/easyprotoc/parser"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

const extensionRangeOptionsType = ".google.protobuf.ExtensionRangeOptions"

func standardDescriptor(name string) protoreflect.Descriptor {
	if !strings.HasPrefix(name, ".google.protobuf.") {
		return nil
	}
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(name[1:]))
	if err != nil || descriptor.ParentFile() != descriptorpb.File_google_protobuf_descriptor_proto {
		return nil
	}
	return descriptor
}

func standardField(message protoreflect.MessageDescriptor, name string) (linker.FieldInfo, bool) {
	field := message.Fields().ByName(protoreflect.Name(name))
	if field == nil {
		return linker.FieldInfo{}, false
	}
	info := linker.FieldInfo{
		Name: name, TagNumber: int32(field.Number()),
		TypeKind: parser.FieldTypeKind(field.Kind()), Label: parser.FieldLabel(field.Cardinality()),
	}
	if field.Message() != nil {
		info.TypeName = "." + string(field.Message().FullName())
	}
	if field.Enum() != nil {
		info.TypeName = "." + string(field.Enum().FullName())
	}
	return info, true
}

// standardOptions lists the fields of each google.protobuf.*Options message in
// descriptor.proto (protoc 36.2) and whether each is a bool.
var standardOptions = map[string]map[string]bool{
	".google.protobuf.FileOptions": {
		"java_package": false, "java_outer_classname": false, "java_multiple_files": true,
		"java_generate_equals_and_hash": true, "java_string_check_utf8": true, "optimize_for": false,
		"go_package": false, "cc_generic_services": true, "java_generic_services": true,
		"py_generic_services": true, "deprecated": true, "cc_enable_arenas": true,
		"objc_class_prefix": false, "csharp_namespace": false, "swift_prefix": false,
		"php_class_prefix": false, "php_namespace": false, "php_metadata_namespace": false,
		"ruby_package": false, "features": false, "uninterpreted_option": false,
	},
	".google.protobuf.MessageOptions": {
		"message_set_wire_format": true, "no_standard_descriptor_accessor": true, "deprecated": true,
		"map_entry": true, "deprecated_legacy_json_field_conflicts": true, "features": false,
		"uninterpreted_option": false,
	},
	".google.protobuf.FieldOptions": {
		"ctype": false, "packed": true, "jstype": false, "lazy": true, "unverified_lazy": true,
		"deprecated": true, "weak": true, "debug_redact": true, "retention": false, "targets": false,
		"edition_defaults": false, "features": false, "feature_support": false, "uninterpreted_option": false,
	},
	".google.protobuf.OneofOptions": {
		"features": false, "uninterpreted_option": false,
	},
	".google.protobuf.EnumOptions": {
		"allow_alias": true, "deprecated": true, "deprecated_legacy_json_field_conflicts": true,
		"features": false, "uninterpreted_option": false,
	},
	".google.protobuf.EnumValueOptions": {
		"deprecated": true, "features": false, "debug_redact": true, "feature_support": false,
		"uninterpreted_option": false,
	},
	".google.protobuf.ServiceOptions": {
		"features": false, "deprecated": true, "uninterpreted_option": false,
	},
	".google.protobuf.MethodOptions": {
		"deprecated": true, "idempotency_level": false, "features": false, "uninterpreted_option": false,
	},
	".google.protobuf.ExtensionRangeOptions": {
		"uninterpreted_option": false, "declaration": false, "features": false, "verification": false,
	},
}
