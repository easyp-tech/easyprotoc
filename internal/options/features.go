package options

import (
	"strings"

	"github.com/easyp-tech/easyprotoc/internal/linker"
	"github.com/easyp-tech/easyprotoc/parser"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const featureSetTypeName = ".google.protobuf.FeatureSet"

// featureOptionTags maps each google.protobuf.*Options message to the field
// number of its `optional FeatureSet features` field in descriptor.proto.
var featureOptionTags = map[string]int32{
	".google.protobuf.FileOptions":           50,
	".google.protobuf.MessageOptions":        12,
	".google.protobuf.FieldOptions":          21,
	".google.protobuf.OneofOptions":          1,
	".google.protobuf.EnumOptions":           7,
	".google.protobuf.EnumValueOptions":      2,
	".google.protobuf.ServiceOptions":        34,
	".google.protobuf.MethodOptions":         35,
	".google.protobuf.ExtensionRangeOptions": 50,
}

// builtinFeatureSetFields provides the schema of google.protobuf.FeatureSet so
// edition files can set standard features.* options without explicitly
// importing google/protobuf/descriptor.proto.
var builtinFeatureSetFields = map[string]linker.FieldInfo{
	"field_presence": {
		Name:      "field_presence",
		TagNumber: 1,
		TypeKind:  parser.TypeEnum,
		TypeName:  ".google.protobuf.FeatureSet.FieldPresence",
		Label:     parser.LabelOptional,
	},
	"enum_type": {
		Name:      "enum_type",
		TagNumber: 2,
		TypeKind:  parser.TypeEnum,
		TypeName:  ".google.protobuf.FeatureSet.EnumType",
		Label:     parser.LabelOptional,
	},
	"repeated_field_encoding": {
		Name:      "repeated_field_encoding",
		TagNumber: 3,
		TypeKind:  parser.TypeEnum,
		TypeName:  ".google.protobuf.FeatureSet.RepeatedFieldEncoding",
		Label:     parser.LabelOptional,
	},
	"utf8_validation": {
		Name:      "utf8_validation",
		TagNumber: 4,
		TypeKind:  parser.TypeEnum,
		TypeName:  ".google.protobuf.FeatureSet.Utf8Validation",
		Label:     parser.LabelOptional,
	},
	"message_encoding": {
		Name:      "message_encoding",
		TagNumber: 5,
		TypeKind:  parser.TypeEnum,
		TypeName:  ".google.protobuf.FeatureSet.MessageEncoding",
		Label:     parser.LabelOptional,
	},
	"json_format": {
		Name:      "json_format",
		TagNumber: 6,
		TypeKind:  parser.TypeEnum,
		TypeName:  ".google.protobuf.FeatureSet.JsonFormat",
		Label:     parser.LabelOptional,
	},
	"enforce_naming_style": {
		Name:      "enforce_naming_style",
		TagNumber: 7,
		TypeKind:  parser.TypeEnum,
		TypeName:  ".google.protobuf.FeatureSet.EnforceNamingStyle",
		Label:     parser.LabelOptional,
	},
	"default_symbol_visibility": {
		Name:      "default_symbol_visibility",
		TagNumber: 8,
		TypeKind:  parser.TypeEnum,
		TypeName:  ".google.protobuf.FeatureSet.VisibilityFeature.DefaultSymbolVisibility",
		Label:     parser.LabelOptional,
	},
	"enforce_proto_limits": {
		Name:      "enforce_proto_limits",
		TagNumber: 9,
		TypeKind:  parser.TypeEnum,
		TypeName:  ".google.protobuf.FeatureSet.ProtoLimitsFeature.EnforceProtoLimits",
		Label:     parser.LabelOptional,
	},
}

var builtinFeatureSetEnums = map[string]map[string]int32{
	".google.protobuf.FeatureSet.FieldPresence": {
		"FIELD_PRESENCE_UNKNOWN": 0,
		"EXPLICIT":               1,
		"IMPLICIT":               2,
		"LEGACY_REQUIRED":        3,
	},
	".google.protobuf.FeatureSet.EnumType": {
		"ENUM_TYPE_UNKNOWN": 0,
		"OPEN":              1,
		"CLOSED":            2,
	},
	".google.protobuf.FeatureSet.RepeatedFieldEncoding": {
		"REPEATED_FIELD_ENCODING_UNKNOWN": 0,
		"PACKED":                          1,
		"EXPANDED":                        2,
	},
	".google.protobuf.FeatureSet.Utf8Validation": {
		"UTF8_VALIDATION_UNKNOWN": 0,
		"VERIFY":                  2,
		"NONE":                    3,
	},
	".google.protobuf.FeatureSet.MessageEncoding": {
		"MESSAGE_ENCODING_UNKNOWN": 0,
		"LENGTH_PREFIXED":          1,
		"DELIMITED":                2,
	},
	".google.protobuf.FeatureSet.JsonFormat": {
		"JSON_FORMAT_UNKNOWN": 0,
		"ALLOW":               1,
		"LEGACY_BEST_EFFORT":  2,
	},
	".google.protobuf.FeatureSet.EnforceNamingStyle": {
		"ENFORCE_NAMING_STYLE_UNKNOWN": 0,
		"STYLE2024":                    1,
		"STYLE_LEGACY":                 2,
		"STYLE2026":                    3,
	},
	".google.protobuf.FeatureSet.VisibilityFeature.DefaultSymbolVisibility": {
		"DEFAULT_SYMBOL_VISIBILITY_UNKNOWN": 0,
		"EXPORT_ALL":                        1,
		"EXPORT_TOP_LEVEL":                  2,
		"LOCAL_ALL":                         3,
		"STRICT":                            4,
	},
	".google.protobuf.FeatureSet.ProtoLimitsFeature.EnforceProtoLimits": {
		"PROTO_LIMITS_UNKNOWN":      0,
		"LEGACY_NO_EXPLICIT_LIMITS": 1,
		"PROTO_LIMITS2026":          2,
	},
}

func lookupMessageField(table *linker.SymbolTable, msgTypeName, fldName string) (linker.FieldInfo, bool) {
	if fields, ok := table.MessageFields[msgTypeName]; ok {
		if info, found := fields[fldName]; found {
			return info, true
		}
	}
	if msgTypeName == featureSetTypeName {
		info, found := builtinFeatureSetFields[fldName]
		return info, found
	}
	if msgTypeName == extensionRangeOptionsType || strings.HasPrefix(msgTypeName, extensionRangeOptionsType+".") {
		if message, ok := standardDescriptor(msgTypeName).(protoreflect.MessageDescriptor); ok {
			return standardField(message, fldName)
		}
	}
	return linker.FieldInfo{}, false
}

func lookupEnumValue(table *linker.SymbolTable, typeName, ident string) (int32, bool) {
	if values, ok := table.EnumValues[typeName]; ok {
		if num, found := values[ident]; found {
			return num, true
		}
	}
	if values, ok := builtinFeatureSetEnums[typeName]; ok {
		if num, found := values[ident]; found {
			return num, true
		}
	}
	if strings.HasPrefix(typeName, extensionRangeOptionsType+".") {
		if enum, ok := standardDescriptor(typeName).(protoreflect.EnumDescriptor); ok {
			if value := enum.Values().ByName(protoreflect.Name(ident)); value != nil {
				return int32(value.Number()), true
			}
		}
	}
	return 0, false
}
