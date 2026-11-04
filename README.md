# easyprotoc

[![ci](https://github.com/easyp-tech/easyprotoc/actions/workflows/ci.yml/badge.svg)](https://github.com/easyp-tech/easyprotoc/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/easyp-tech/easyprotoc?color=blue)](LICENSE)

A Protocol Buffers compiler written in Go. It is part of [EasyP](https://github.com/easyp-tech/easyp).

`easyprotoc` reads `.proto` files and writes the same `FileDescriptorSet` that
`protoc` writes, including source info, comments and custom options. It is
also a library: compile to `protoreflect` descriptors or to a plugin
`CodeGeneratorRequest`, link without building descriptors, or parse a single
file for linting.

> **Status: experimental.** The output is checked against protoc on all of
> googleapis on every CI run. The Go API may change before v1.

## Install

```sh
go install github.com/easyp-tech/easyprotoc/cmd/easyprotoc@latest
```

## Command line

The flags follow protoc:

```sh
easyprotoc -I . -I third_party \
  --include_imports --include_source_info \
  --descriptor_set_out=out.binpb \
  api/v1/service.proto
```

| Flag | Meaning |
|---|---|
| `-I<dir>`, `--proto_path=<dir>` | import path, may repeat |
| `-o<file>`, `--descriptor_set_out=<file>` | write a `FileDescriptorSet` |
| `--include_imports` | add every imported file to the set |
| `--include_source_info` | keep locations and comments |
| `@<file>` | read more arguments from a file, one per line |
| `--threads=<n>` | how many files compile at once (default: all CPUs) |

easyprotoc does not run plugins (`--go_out` and so on). It writes descriptor
sets. To run plugins, use [easyp](https://github.com/easyp-tech/easyp)
`generate`, or the `CodeGeneratorRequest` from the library.

## Library

```go
result, err := protoc.Compile(ctx, []string{"greet/v1/greet.proto"},
	protoc.WithImportPaths("proto"),
	protoc.WithStandardImports(true), // google/protobuf/*.proto built in
	protoc.WithIncludeImports(true),
)
if err != nil {
	return err
}
set := result.DescriptorSet()        // *descriptorpb.FileDescriptorSet
files := result.Files()              // []protoreflect.FileDescriptor
req, err := result.CodeGeneratorRequest("paths=source_relative")
```

Packages:

| Package | What it is for |
|---|---|
| [`protoc`](protoc) | compile, link, diagnostics, resolvers (`DirResolver`, `MemResolver`, `MultiResolver`) |
| [`protoc/syntax`](protoc/syntax) | parse one file without linking, with tokens and comments (used by `easyp lint`) |
| [`parser`](parser), [`lexer`](lexer) | the syntax tree and tokens behind `protoc/syntax` |

Errors and warnings come as `protoc.Diagnostic` values with file, line and
column. Their text follows protoc.

## Benchmarks

Wall time, median of 10 runs after 2 warmups, and peak memory. All three
write the same set: `--include_imports --include_source_info`.
Apple M5 Pro (18 cores), macOS 27.2; protoc 36.2, protocompile
`v0.14.2-0.20261020145055-56248e843b50` (the version in buf v1.75.0),
googleapis `8f11fef`. Full report: [`bench/results/20261104-darwin-arm64.md`](bench/results/20261104-darwin-arm64.md).

| corpus | files in the set | protoc | protocompile | easyprotoc |
|---|---:|---:|---:|---:|
| `descriptor.proto` alone | 1 | 8.6 ms, 10 MiB | 8.6 ms, 14 MiB | **5.8 ms**, 10 MiB |
| `compute.proto` (one 4 MB file) | 11 | 174 ms, 128 MiB | 282 ms, 132 MiB | **103 ms**, 75 MiB |
| google/{api,rpc,type,longrunning} | 103 | 36 ms, 21 MiB | **27 ms**, 42 MiB | 31 ms, 21 MiB |
| all of googleapis | 7427 | 3.74 s, 904 MiB | 1.31 s, 3033 MiB | **0.91 s**, 804 MiB |

On all of googleapis easyprotoc is 4.1 times faster than protoc and 1.4 times
faster than protocompile with a quarter of its memory. On a small project
protocompile is a little faster. protoc compiles on one thread; protocompile
and easyprotoc use every core, so their CPU time is higher than their wall
time.

## Reproduce

You need Go and git. The first run downloads protoc (its sha256 is checked
against [`bench/checksums.txt`](bench/checksums.txt)) and googleapis at a fixed
commit into `bench/.cache`.

```sh
bench/run.sh          # time protoc, protocompile and easyprotoc on every corpus
bench/run.sh parity   # check that easyprotoc writes the same descriptors as protoc
```

`RUNS`, `CORPORA` and `IMPLS` narrow a run, and `CANDIDATE=/path/to/binary`
adds any other protoc-compatible compiler to the table. Results go to
`bench/results/`. See the top of [`bench/run.sh`](bench/run.sh) for the rest.

## License

[Apache 2.0](LICENSE). The well-known types in [`protoc/wkt`](protoc/wkt) are
from Protocol Buffers and keep their own [BSD license](protoc/wkt/LICENSE).
