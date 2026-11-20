module github.com/easyp-tech/easyprotoc/bench

go 1.26.7

require (
	github.com/bufbuild/protocompile v0.14.1
	github.com/easyp-tech/easyprotoc v0.0.0
	github.com/google/go-cmp v0.7.0
	google.golang.org/protobuf v1.36.12
)

require golang.org/x/sync v0.22.0 // indirect

replace github.com/easyp-tech/easyprotoc => ../
