package protoc_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/easyp-tech/easyprotoc/protoc"
)

type funcResolver func(ctx context.Context, path string) ([]byte, error)

func (f funcResolver) FindFileByPath(ctx context.Context, path string) ([]byte, error) {
	return f(ctx, path)
}

func TestCompileStrictResolverNoCWDFallback(t *testing.T) {
	cwdDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(cwdDir, "leaked.proto"),
		[]byte(`syntax = "proto3"; package leaked.v1; message Secret { string x = 1; }`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwdDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWD); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	rootDir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(rootDir, "app.proto"),
		[]byte(`syntax = "proto3"; package app.v1; import "leaked.proto"; message App { leaked.v1.Secret s = 1; }`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	resolver := funcResolver(func(_ context.Context, path string) ([]byte, error) {
		if strings.HasSuffix(path, "leaked.proto") {
			return nil, protoc.ErrFileNotFound
		}
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, protoc.ErrFileNotFound
			}
			return nil, err
		}
		return b, nil
	})

	_, err = protoc.Compile(
		context.Background(),
		[]string{"app.proto"},
		protoc.WithImportPaths(rootDir),
		protoc.WithResolver(resolver),
		protoc.WithStandardImports(true),
	)
	if err == nil {
		t.Fatal("expected compilation to fail instead of reading leaked.proto from CWD")
	}
}

func TestCompilePreservesResolverErrors(t *testing.T) {
	t.Parallel()

	errBlocked := errors.New("source target is outside root")
	appSrc := []byte(`syntax = "proto3"; package app.v1; import "escape.proto"; message App {}`)

	t.Run("imported_file_error", func(t *testing.T) {
		t.Parallel()
		resolver := funcResolver(func(_ context.Context, path string) ([]byte, error) {
			switch path {
			case "app.proto":
				return appSrc, nil
			case "escape.proto":
				return nil, errBlocked
			default:
				return nil, protoc.ErrFileNotFound
			}
		})

		_, err := protoc.Compile(
			context.Background(),
			[]string{"app.proto"},
			protoc.WithResolver(resolver),
			protoc.WithStandardImports(true),
		)
		if !errors.Is(err, errBlocked) {
			t.Fatalf("expected error wrapping %v, got %v", errBlocked, err)
		}
	})

	t.Run("root_file_error", func(t *testing.T) {
		t.Parallel()
		resolver := funcResolver(func(_ context.Context, path string) ([]byte, error) {
			if path == "escape.proto" {
				return nil, errBlocked
			}
			return nil, protoc.ErrFileNotFound
		})

		_, err := protoc.Compile(
			context.Background(),
			[]string{"escape.proto"},
			protoc.WithResolver(resolver),
		)
		if !errors.Is(err, errBlocked) {
			t.Fatalf("expected error wrapping %v, got %v", errBlocked, err)
		}
	})
}

func TestCompileEditions2023Features(t *testing.T) {
	t.Parallel()

	t.Run("standard_and_cpp_extension_features", func(t *testing.T) {
		t.Parallel()
		src := `edition = "2023";
package ed.v1;
import "google/protobuf/cpp_features.proto";

option features.enum_type = CLOSED;

message M {
  option features.json_format = LEGACY_BEST_EFFORT;
  string implicit_str = 1 [features.field_presence = IMPLICIT];
  string no_utf8 = 2 [features.utf8_validation = NONE];
  string cpp_view = 3 [features.(pb.cpp).string_type = VIEW];
  string combined = 4 [features.field_presence = IMPLICIT, features.(pb.cpp).string_type = CORD];
}
`
		res, err := protoc.Compile(
			context.Background(),
			[]string{"ed.proto"},
			protoc.WithResolver(protoc.MemResolver{"ed.proto": []byte(src)}),
			protoc.WithStandardImports(true),
			protoc.WithIncludeImports(true),
			protoc.WithIncludeSourceInfo(true),
		)
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}

		files, err := res.Files()
		if err != nil {
			t.Fatalf("Files failed: %v", err)
		}

		var fd protoreflect.FileDescriptor
		for _, f := range files {
			if f.Path() == "ed.proto" {
				fd = f
				break
			}
		}
		if fd == nil {
			t.Fatal("ed.proto not found in Result.Files()")
		}
		if fd.Syntax() != protoreflect.Editions {
			t.Fatalf("Syntax() = %v, want Editions", fd.Syntax())
		}

		var fdp *descriptorpb.FileDescriptorProto
		for _, f := range res.DescriptorSet().GetFile() {
			if f.GetName() == "ed.proto" {
				fdp = f
				break
			}
		}
		if fdp == nil {
			t.Fatal("ed.proto not found in DescriptorSet()")
		}
		if fdp.GetEdition() != descriptorpb.Edition_EDITION_2023 {
			t.Fatalf("Edition = %v, want EDITION_2023", fdp.GetEdition())
		}
		if got := fdp.GetOptions().GetFeatures().GetEnumType(); got != descriptorpb.FeatureSet_CLOSED {
			t.Fatalf("file enum_type = %v, want CLOSED", got)
		}

		msg := fd.Messages().ByName("M")
		if msg == nil {
			t.Fatal("message M missing")
		}
		msgOpts := msg.Options().(*descriptorpb.MessageOptions)
		if got := msgOpts.GetFeatures().GetJsonFormat(); got != descriptorpb.FeatureSet_LEGACY_BEST_EFFORT {
			t.Fatalf("message json_format = %v, want LEGACY_BEST_EFFORT", got)
		}

		implicitField := msg.Fields().ByName("implicit_str")
		if implicitField == nil {
			t.Fatal("field implicit_str missing")
		}
		if implicitField.HasPresence() {
			t.Fatal("implicit_str.HasPresence() = true, want false")
		}

		noUTF8Field := msg.Fields().ByName("no_utf8")
		if noUTF8Field == nil {
			t.Fatal("field no_utf8 missing")
		}
		noUTF8Opts := noUTF8Field.Options().(*descriptorpb.FieldOptions)
		if got := noUTF8Opts.GetFeatures().GetUtf8Validation(); got != descriptorpb.FeatureSet_NONE {
			t.Fatalf("no_utf8 utf8_validation = %v, want NONE", got)
		}

		checkCppStringType := func(fieldName, want string) {
			t.Helper()
			fld := msg.Fields().ByName(protoreflect.Name(fieldName))
			if fld == nil {
				t.Fatalf("field %s missing", fieldName)
			}
			feats := fld.Options().(*descriptorpb.FieldOptions).GetFeatures()
			if feats == nil {
				t.Fatalf("field %s features nil", fieldName)
			}
			got := ""
			feats.ProtoReflect().Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
				if string(f.FullName()) != "pb.cpp" || f.Kind() != protoreflect.MessageKind {
					return true
				}
				member := v.Message().Descriptor().Fields().ByName("string_type")
				if member != nil && v.Message().Has(member) {
					if ev := member.Enum().Values().ByNumber(v.Message().Get(member).Enum()); ev != nil {
						got = string(ev.Name())
					}
				}
				return false
			})
			if got != want {
				t.Fatalf("field %s pb.cpp.string_type = %q, want %q", fieldName, got, want)
			}
		}

		checkCppStringType("cpp_view", "VIEW")
		checkCppStringType("combined", "CORD")
		if msg.Fields().ByName("combined").HasPresence() {
			t.Fatal("combined.HasPresence() = true, want false")
		}
	})

	t.Run("standalone_without_descriptor_proto_import", func(t *testing.T) {
		t.Parallel()
		src := `edition = "2023";
package standalone.v1;

option features.field_presence = IMPLICIT;

message M {
  option features = { json_format: LEGACY_BEST_EFFORT };
  string s = 1 [features.utf8_validation = NONE];
}
`
		res, err := protoc.Compile(
			context.Background(),
			[]string{"standalone.proto"},
			protoc.WithResolver(protoc.MemResolver{"standalone.proto": []byte(src)}),
		)
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		files, err := res.Files()
		if err != nil {
			t.Fatalf("Files failed: %v", err)
		}
		if len(files) != 1 {
			t.Fatalf("len(files) = %d, want 1", len(files))
		}
		msg := files[0].Messages().ByName("M")
		if msg.Fields().ByName("s").HasPresence() {
			t.Fatal("expected inherited file-level IMPLICIT field presence")
		}
		if got := msg.Options().(*descriptorpb.MessageOptions).GetFeatures().GetJsonFormat(); got != descriptorpb.FeatureSet_LEGACY_BEST_EFFORT {
			t.Fatalf("json_format = %v, want LEGACY_BEST_EFFORT", got)
		}
	})

	t.Run("unknown_feature_reports_error", func(t *testing.T) {
		t.Parallel()
		src := `edition = "2023";
package bad.v1;

message M {
  string s = 1 [features.no_such_feature = IMPLICIT];
}
`
		var diags []protoc.Diagnostic
		_, err := protoc.Compile(
			context.Background(),
			[]string{"bad.proto"},
			protoc.WithResolver(protoc.MemResolver{"bad.proto": []byte(src)}),
			protoc.WithErrorHandler(func(d protoc.Diagnostic) { diags = append(diags, d) }),
		)
		if !errors.Is(err, protoc.ErrCompilationFailed) {
			t.Fatalf("expected ErrCompilationFailed, got %v", err)
		}
		if len(diags) == 0 || !strings.Contains(diags[0].Message, "no_such_feature") {
			t.Fatalf("expected diagnostic mentioning no_such_feature, got %+v", diags)
		}
	})
}

func TestResultFilesReturnsErrorWhenImportMissingFromDescriptorSet(t *testing.T) {
	t.Parallel()

	src := `syntax = "proto3";
package p.v1;
import "google/protobuf/timestamp.proto";
message M { google.protobuf.Timestamp at = 1; }
`
	res, err := protoc.Compile(
		context.Background(),
		[]string{"p.proto"},
		protoc.WithResolver(protoc.MemResolver{"p.proto": []byte(src)}),
		protoc.WithStandardImports(true),
		protoc.WithIncludeImports(false),
	)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	_, err = res.Files()
	if err == nil {
		t.Fatal("expected error from Result.Files() when imported dependency is omitted from DescriptorSet")
	}
}
