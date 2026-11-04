// Command protocompile-cli wraps github.com/bufbuild/protocompile in a
// protoc-compatible command line so it can be benchmarked and verified the
// same way as protoc and our own compiler.
//
// Supported flags (subset of protoc):
//
//	-I<dir> | -I <dir> | --proto_path=<dir>
//	-o<file> | --descriptor_set_out=<file>
//	--include_imports
//	--include_source_info
//	@<file>             read additional arguments, one per line
//
// Extra (non-protoc) flags:
//
//	--parallelism=<n>         protocompile MaxParallelism (default GOMAXPROCS)
//	--metrics_out=<file>      write easyp.compiler.metrics/v1 JSON
//	--metrics_mode=<mode>     parallel (default): normal run, totals only;
//	                          trace: sequential run with per-file, per-phase timings
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/linker"
	"github.com/bufbuild/protocompile/protoutil"

	"github.com/easyp-tech/easyprotoc/bench/internal/metrics"
)

type options struct {
	importPaths    []string
	out            string
	includeImports bool
	sourceInfo     bool
	parallelism    int
	metricsOut     string
	metricsMode    string
	files          []string
}

func main() {
	start := time.Now()
	opts := &options{metricsMode: "parallel"}
	if err := parseArgs(os.Args[1:], opts); err != nil {
		fatalf("%v", err)
	}
	if len(opts.files) == 0 {
		fatalf("no input files")
	}

	report := &metrics.Report{
		Impl:    "protocompile",
		Mode:    opts.metricsMode,
		Threads: runtime.GOMAXPROCS(0),
		Totals:  metrics.Totals{Phases: map[string]int64{}},
	}
	var (
		files   linker.Files
		perFile map[string]*metrics.File
		err     error
	)
	switch opts.metricsMode {
	case "parallel":
		t := time.Now()
		files, err = compile(opts)
		report.Totals.Phases[metrics.PhaseCompile] = time.Since(t).Nanoseconds()
	case "trace":
		report.Threads = 1
		files, perFile, err = traceCompile(opts)
	default:
		err = fmt.Errorf("unknown --metrics_mode %q", opts.metricsMode)
	}
	if err != nil {
		fatalf("%v", err)
	}

	if opts.out != "" {
		t := time.Now()
		data, err := serialize(files, opts.includeImports, perFile)
		if err != nil {
			fatalf("marshal: %v", err)
		}
		report.Totals.Phases[metrics.PhaseSerialize] = time.Since(t).Nanoseconds()
		report.Totals.OutputBytes = int64(len(data))

		t = time.Now()
		if err := os.WriteFile(opts.out, data, 0o644); err != nil {
			fatalf("%v", err)
		}
		report.Totals.Phases[metrics.PhaseWrite] = time.Since(t).Nanoseconds()
	}

	if opts.metricsOut == "" {
		return
	}
	report.Totals.WallNs = time.Since(start).Nanoseconds()
	if perFile != nil {
		// Trace mode: totals per phase are sums of exclusive per-file times.
		for _, f := range perFile {
			report.Files = append(report.Files, *f)
			report.Totals.Files++
			report.Totals.InputBytes += f.Bytes
			for p, ns := range f.Phases {
				if p != metrics.PhaseSerialize {
					report.Totals.Phases[p] += ns
				}
			}
		}
	} else {
		report.Totals.Files = len(files)
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	report.Memory = &metrics.Memory{
		TotalAllocBytes: int64(ms.TotalAlloc),
		Mallocs:         int64(ms.Mallocs),
		PeakHeapBytes:   int64(ms.HeapSys),
		GCCycles:        int64(ms.NumGC),
		GCPauseNs:       int64(ms.PauseTotalNs),
	}
	if err := metrics.Write(opts.metricsOut, report); err != nil {
		fatalf("%v", err)
	}
}

func compile(opts *options) (linker.Files, error) {
	mode := protocompile.SourceInfoNone
	if opts.sourceInfo {
		mode = protocompile.SourceInfoStandard
	}
	c := protocompile.Compiler{
		// Files on the import path win over protocompile's embedded WKT copies
		// (which differ slightly from the ones shipped with protoc). Note that
		// descriptor.proto still comes out as protocompile's own copy.
		Resolver: protocompile.CompositeResolver{
			&protocompile.SourceResolver{ImportPaths: opts.importPaths},
			protocompile.WithStandardImports(&protocompile.SourceResolver{}),
		},
		SourceInfoMode: mode,
		MaxParallelism: opts.parallelism,
	}
	return c.Compile(context.Background(), opts.files...)
}

// serialize writes a FileDescriptorSet by hand (field 1, one entry per file)
// so the cost of each file can be attributed when perFile is non-nil. The
// bytes are identical to marshaling the set deterministically.
func serialize(files linker.Files, includeImports bool, perFile map[string]*metrics.File) ([]byte, error) {
	var out []byte
	seen := make(map[string]bool)
	var add func(fd protoreflect.FileDescriptor) error
	add = func(fd protoreflect.FileDescriptor) error {
		if seen[fd.Path()] {
			return nil
		}
		seen[fd.Path()] = true
		if includeImports {
			// protoc emits dependencies before dependents.
			imports := fd.Imports()
			for i := range imports.Len() {
				if err := add(imports.Get(i).FileDescriptor); err != nil {
					return err
				}
			}
		}
		t := time.Now()
		b, err := proto.MarshalOptions{Deterministic: true}.Marshal(protoutil.ProtoFromFileDescriptor(fd))
		if err != nil {
			return err
		}
		out = protowire.AppendTag(out, 1, protowire.BytesType)
		out = protowire.AppendBytes(out, b)
		if f := perFile[fd.Path()]; f != nil {
			ns := time.Since(t).Nanoseconds()
			f.Phases[metrics.PhaseSerialize] = ns
			f.TotalNs += ns
			f.OutputBytes = int64(len(b))
		}
		return nil
	}
	for _, fd := range files {
		if err := add(fd); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func parseArgs(args []string, opts *options) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case strings.HasPrefix(arg, "@"):
			more, err := readArgFile(arg[1:])
			if err != nil {
				return err
			}
			if err := parseArgs(more, opts); err != nil {
				return err
			}
		case arg == "-I":
			i++
			if i >= len(args) {
				return fmt.Errorf("-I requires an argument")
			}
			opts.importPaths = append(opts.importPaths, args[i])
		case strings.HasPrefix(arg, "-I"):
			opts.importPaths = append(opts.importPaths, arg[2:])
		case strings.HasPrefix(arg, "--proto_path="):
			opts.importPaths = append(opts.importPaths, strings.TrimPrefix(arg, "--proto_path="))
		case strings.HasPrefix(arg, "--descriptor_set_out="):
			opts.out = strings.TrimPrefix(arg, "--descriptor_set_out=")
		case strings.HasPrefix(arg, "-o"):
			opts.out = arg[2:]
		case arg == "--include_imports":
			opts.includeImports = true
		case arg == "--include_source_info":
			opts.sourceInfo = true
		case strings.HasPrefix(arg, "--parallelism="):
			n, err := strconv.Atoi(strings.TrimPrefix(arg, "--parallelism="))
			if err != nil {
				return fmt.Errorf("bad --parallelism: %w", err)
			}
			opts.parallelism = n
		case strings.HasPrefix(arg, "--metrics_out="):
			opts.metricsOut = strings.TrimPrefix(arg, "--metrics_out=")
		case strings.HasPrefix(arg, "--metrics_mode="):
			opts.metricsMode = strings.TrimPrefix(arg, "--metrics_mode=")
		case strings.HasPrefix(arg, "-"):
			return fmt.Errorf("unsupported flag %q", arg)
		default:
			opts.files = append(opts.files, arg)
		}
	}
	return nil
}

func readArgFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			out = append(out, line)
		}
	}
	return out, sc.Err()
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "protocompile-cli: "+format+"\n", args...)
	os.Exit(1)
}
