package protoc

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestCompileKeepsExtensionRangeOptions(t *testing.T) {
	t.Parallel()
	result, err := Compile(t.Context(), []string{"range.proto"}, WithResolverOnly(MemResolver{
		"range.proto": []byte(`syntax = "proto2";
package sample;
message Message {
  extensions 100 to 199 [
    declaration = { number: 100, full_name: ".sample.ext", type: "string" },
    declaration = { number: 104, reserved: true },
    verification = DECLARATION
  ];
}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	opts := result.DescriptorSet().File[0].MessageType[0].ExtensionRange[0].GetOptions()
	declarations := opts.GetDeclaration()
	if len(declarations) != 2 ||
		declarations[0].GetNumber() != 100 || declarations[0].GetFullName() != ".sample.ext" || declarations[0].GetType() != "string" ||
		declarations[1].GetNumber() != 104 || !declarations[1].GetReserved() {
		t.Fatalf("declarations = %v", declarations)
	}
	if opts.Verification == nil || opts.GetVerification() != descriptorpb.ExtensionRangeOptions_DECLARATION {
		t.Fatalf("verification = %v", opts)
	}
}

func TestCompileKeepsEditionFeatures(t *testing.T) {
	t.Parallel()
	result, err := Compile(t.Context(), []string{"edition.proto"}, WithResolverOnly(MemResolver{
		"edition.proto": []byte(`edition = "2023";
option features.field_presence = IMPLICIT;
message Record { int32 value = 1 [features.field_presence = EXPLICIT]; }`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	file := result.DescriptorSet().File[0]
	if got := file.GetOptions().GetFeatures().GetFieldPresence(); got != descriptorpb.FeatureSet_IMPLICIT {
		t.Fatalf("file field_presence = %v, want IMPLICIT", got)
	}
	if got := file.MessageType[0].Field[0].GetOptions().GetFeatures().GetFieldPresence(); got != descriptorpb.FeatureSet_EXPLICIT {
		t.Fatalf("field field_presence = %v, want EXPLICIT", got)
	}
}

func TestCompileKeepsExplicitDefaults(t *testing.T) {
	t.Parallel()
	result, err := Compile(t.Context(), []string{"defaults.proto"}, WithResolverOnly(MemResolver{
		"defaults.proto": []byte(`syntax = "proto2";
message Record {
  optional string text = 1 [default = ""];
  optional bytes data = 2 [default = "\000\377\n\\"];
}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	file, err := protodesc.NewFile(result.DescriptorSet().File[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	fields := file.Messages().Get(0).Fields()
	if field := fields.ByName("text"); !field.HasDefault() || field.Default().String() != "" {
		t.Fatalf("text.HasDefault() = %v, default = %q", field.HasDefault(), field.Default())
	}
	if data := fields.ByName("data").Default().Bytes(); !bytes.Equal(data, []byte{0, 255, '\n', '\\'}) {
		t.Fatalf("data default = %q", data)
	}
}

func TestCompileKeepsNestedExtensionScope(t *testing.T) {
	t.Parallel()
	result, err := Compile(t.Context(), []string{"nested.proto"}, WithResolverOnly(MemResolver{
		"nested.proto": []byte(`syntax = "proto2";
package sample;
message Target { extensions 100 to 199; }
message Holder {
  message Value {}
  extend Target { optional Value nested = 100; }
}`),
	}))
	if err != nil {
		t.Fatal(err)
	}
	file := result.DescriptorSet().File[0]
	extensions := file.MessageType[1].GetExtension()
	if len(file.GetExtension()) != 0 || len(extensions) != 1 ||
		extensions[0].GetTypeName() != ".sample.Holder.Value" || extensions[0].GetExtendee() != ".sample.Target" {
		t.Fatalf("file extensions = %v, Holder extensions = %v", file.GetExtension(), extensions)
	}
}
