package protoc

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"testing"
)

func TestCompileResolverOnlyDoesNotReadDisk(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("blocked.proto", []byte(`syntax = "proto3"; message Blocked {}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, cause := range []error{ErrFileNotFound, fs.ErrPermission} {
		t.Run(cause.Error(), func(t *testing.T) {
			resolve := ResolverFunc(func(_ context.Context, path string) ([]byte, error) {
				if path == "entry.proto" {
					return []byte(`syntax = "proto3"; import "blocked.proto"; message Entry { Blocked value = 1; }`), nil
				}
				return nil, cause
			})
			var diagnostics []Diagnostic
			result, err := Compile(t.Context(), []string{"./entry.proto"},
				WithImportPaths(dir), WithResolverOnly(resolve), WithErrorHandler(func(d Diagnostic) {
					diagnostics = append(diagnostics, d)
				}),
			)
			if result != nil || !errors.Is(err, cause) || !errors.Is(err, ErrCompilationFailed) {
				t.Fatalf("Compile = %v, %v, want failure wrapping %v", result, err, cause)
			}
			if len(diagnostics) != 1 || diagnostics[0].Path != "entry.proto" {
				t.Fatalf("diagnostics = %v, want an import diagnostic at entry.proto", diagnostics)
			}
		})
	}
}
