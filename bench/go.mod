module github.com/easyp-tech/easyprotoc/bench

go 1.26.7

require (
	github.com/bufbuild/protocompile v0.14.2-0.20261020145055-56248e843b50
	github.com/easyp-tech/easyprotoc v0.0.0
	github.com/google/go-cmp v0.7.0
	google.golang.org/protobuf v1.37.0-rc.1
)

require (
	github.com/petermattis/goid v0.0.0-20260716134002-a9b348f0a2b9 // indirect
	golang.org/x/sync v0.22.0 // indirect
)

replace github.com/easyp-tech/easyprotoc => ../
