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

Wall time (median of 10 runs after 2 warmups) and peak memory, on an Apple
M5 Pro (18 cores), macOS 27.2. protoc 36.2; protocompile
`v0.14.2-0.20261020145055-56248e843b50`, the version in buf v1.75.0;
googleapis at `8f11fef`. Full report with min, spread and CPU time:
[`bench/results/20261104-darwin-arm64.md`](bench/results/20261104-darwin-arm64.md).

Corpora:
- **descriptor.proto**: one small file, mostly process startup;
- **compute.proto**: one 4 MB file;
- **api-common**: `google/{api,rpc,type,longrunning}`, 91 files;
- **googleapis**: all of `google/` and `grafeas/`, 7416 files.

protoc compiles on one thread. protocompile and easyprotoc use every core, so
their CPU time is higher than their wall time.

### Descriptor set with source info

`--include_imports --include_source_info`, the set buf and easyp build. The
output is the same as protoc's.

| corpus | protoc | protocompile | easyprotoc | vs protoc | vs protocompile |
|---|---:|---:|---:|---:|---:|
| descriptor.proto | 8.6 ms, 10 MiB | 7.9 ms, 14 MiB | **5.4 ms**, 10 MiB | 1.6× | 1.5× |
| compute.proto | 172 ms, 129 MiB | 292 ms, 135 MiB | **89 ms**, 73 MiB | 1.9× | 3.3× |
| api-common | 42 ms, 22 MiB | **23 ms**, 42 MiB | 29 ms, 22 MiB | 1.4× | 0.8× |
| googleapis | 3.83 s, 955 MiB | 1.27 s, 3095 MiB | **0.88 s**, 801 MiB | 4.3× | 1.4× |

### Descriptor set without source info

`--include_imports` only: no locations and no comments.

| corpus | protoc | protocompile | easyprotoc | vs protoc | vs protocompile |
|---|---:|---:|---:|---:|---:|
| descriptor.proto | 7.6 ms, 10 MiB | 6.1 ms, 11 MiB | **4.4 ms**, 8 MiB | 1.7× | 1.4× |
| compute.proto | 125 ms, 97 MiB | 220 ms, 93 MiB | **33 ms**, 33 MiB | 3.8× | 6.8× |
| api-common | 26 ms, 15 MiB | 17 ms, 33 MiB | **12 ms**, 16 MiB | 2.1× | 1.4× |
| googleapis | 2.88 s, 637 MiB | 1.06 s, 1993 MiB | **0.54 s**, 561 MiB | 5.4× | 2.0× |

### Parsing only

Every file is parsed on its own, without imports or linking, the way a linter
reads them: `protoc/syntax.Parse` against protocompile's `parser.Parse`.
protoc cannot do this.

| corpus | protocompile | easyprotoc | vs protocompile |
|---|---:|---:|---:|
| descriptor.proto | 4.8 ms, 8 MiB | **4.2 ms**, 7 MiB | 1.1× |
| compute.proto | 40 ms, 40 MiB | **16 ms**, 32 MiB | 2.5× |
| api-common | 10.6 ms, 18 MiB | **7.1 ms**, 16 MiB | 1.5× |
| googleapis | 492 ms, 171 MiB | **223 ms**, 116 MiB | 2.2× |

### Inside the easyp CLI

[easyp](https://github.com/easyp-tech/easyp) v1 moved to easyprotoc in a
branch that is not released yet. Here the current v1 nightly (protocompile,
and go-protoparser for lint) is compared with that branch and with buf 1.57.0, on
the same machine, median of 3 runs (the old lint on googleapis: 1 run).
googleapis here leaves out the two 4 MB `compute.proto` files (7414 files).
`easyp lint` and `buf lint` do not run the same rule set, so the lint rows
compare speed only.

| command | corpus | easyp v1 nightly | easyp + easyprotoc | buf |
|---|---|---:|---:|---:|
| lint | googleapis | 56.2 s, 99 MiB | **0.85 s**, 787 MiB | 4.74 s, 4914 MiB |
| lint | compute.proto | about 20 min | **0.08 s**, 83 MiB | 0.49 s, 394 MiB |
| lint | api-common | 0.67 s, 39 MiB | **0.03 s**, 43 MiB | 0.10 s, 118 MiB |
| breaking | googleapis | 14.0 s, 3757 MiB | **4.8 s**, 2405 MiB | 9.2 s, 7023 MiB |
| descriptor set | googleapis | 7.75 s, 2754 MiB | **1.48 s**, 1421 MiB | 2.14 s, 2377 MiB |

The old lint spent 20 minutes on `compute.proto` because go-protoparser
attaches comments in quadratic time. This table was measured in the easyp
development tree and cannot be reproduced from this repository yet; the
tables above can.

## Reproduce

You need Go and git. The first run downloads protoc (its sha256 is checked
against [`bench/checksums.txt`](bench/checksums.txt)) and googleapis at a fixed
commit into `bench/.cache`.

```sh
bench/run.sh          # all three tables: with source info, without it, parsing only
bench/run.sh parity   # compare descriptors against protoc --retain_options
```

The parity check passes `--retain_options` to `protoc` because easyprotoc
includes source-retention options in its descriptor sets.

`MODES` (`full nosrc parse`), `RUNS`, `CORPORA` and `IMPLS` narrow a run, and `CANDIDATE=/path/to/binary`
adds any other protoc-compatible compiler to the table. Results go to
`bench/results/`. See the top of [`bench/run.sh`](bench/run.sh) for the rest.

## License

[Apache 2.0](LICENSE). The well-known types in [`protoc/wkt`](protoc/wkt) are
from Protocol Buffers and keep their own [BSD license](protoc/wkt/LICENSE).
