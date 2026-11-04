package protoc_test

import (
	"context"
	"fmt"

	"github.com/easyp-tech/easyprotoc/protoc"
)

func ExampleCompile() {
	files := protoc.MemResolver{
		"greet/v1/greet.proto": []byte(`syntax = "proto3";
package greet.v1;
import "google/protobuf/timestamp.proto";
message Hello {
  string name = 1;
  google.protobuf.Timestamp at = 2;
}`),
	}
	result, err := protoc.Compile(context.Background(), []string{"greet/v1/greet.proto"},
		protoc.WithResolver(files),
		protoc.WithStandardImports(true),
		protoc.WithIncludeImports(true),
	)
	if err != nil {
		panic(err)
	}
	for _, file := range result.DescriptorSet().GetFile() {
		fmt.Println(file.GetName())
	}
	// Output:
	// google/protobuf/timestamp.proto
	// greet/v1/greet.proto
}
