package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/bufbuild/protocompile/linker"
	protooptions "github.com/bufbuild/protocompile/options"
	"github.com/bufbuild/protocompile/parser"
	"github.com/bufbuild/protocompile/reporter"
	"github.com/bufbuild/protocompile/sourceinfo"

	"github.com/easyp-tech/easyprotoc/bench/internal/metrics"
)

// traceCompile runs protocompile's pipeline stage by stage, single-threaded,
// mirroring what protocompile.Compiler does per file (see compiler.go,
// task.link), and records exclusive per-file timings for each stage.
//
// It is slower than the real (parallel) compile and skips a few corner cases
// (e.g. overriding descriptor.proto), so use it to find where time goes, not
// as the headline number.
func traceCompile(opts *options) (linker.Files, map[string]*metrics.File, error) {
	tr := &tracer{
		opts:    opts,
		handler: reporter.NewHandler(nil),
		symbols: &linker.Symbols{},
		done:    map[string]linker.File{},
		perFile: map[string]*metrics.File{},
	}
	var out linker.Files
	for _, path := range opts.files {
		f, err := tr.load(path)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, f)
	}
	return out, tr.perFile, nil
}

type tracer struct {
	opts    *options
	handler *reporter.Handler
	symbols *linker.Symbols
	done    map[string]linker.File
	loading []string
	perFile map[string]*metrics.File
}

func (tr *tracer) load(path string) (linker.File, error) {
	if f, ok := tr.done[path]; ok {
		return f, nil
	}
	for _, p := range tr.loading {
		if p == path {
			return nil, fmt.Errorf("import cycle through %s", path)
		}
	}
	tr.loading = append(tr.loading, path)
	defer func() { tr.loading = tr.loading[:len(tr.loading)-1] }()

	m := &metrics.File{Path: path, Phases: map[string]int64{}}
	lap := stopwatch(m)

	data, err := tr.read(path)
	if err != nil {
		return nil, err
	}
	m.Bytes = int64(len(data))
	m.Lines = int64(bytes.Count(data, []byte{'\n'}))
	lap(metrics.PhaseRead)

	ast, err := parser.Parse(path, bytes.NewReader(data), tr.handler)
	if err != nil {
		return nil, err
	}
	lap(metrics.PhaseParse)
	res, err := parser.ResultFromAST(ast, true, tr.handler)
	if err != nil {
		return nil, err
	}
	lap(metrics.PhaseDescriptor)

	// Imports are loaded outside this file's stopwatch so timings stay exclusive.
	var deps linker.Files
	for _, dep := range res.FileDescriptorProto().GetDependency() {
		f, err := tr.load(dep)
		if err != nil {
			return nil, err
		}
		deps = append(deps, f)
	}
	lap("")

	linked, err := linker.Link(res, deps, tr.symbols, tr.handler)
	if err != nil {
		return nil, err
	}
	lap(metrics.PhaseLink)

	optsIndex, err := protooptions.InterpretOptions(linked, tr.handler)
	if err != nil {
		return nil, err
	}
	if err := linked.ValidateOptions(tr.handler, tr.symbols); err != nil {
		return nil, err
	}
	if err := tr.handler.Error(); err != nil {
		return nil, err
	}
	lap(metrics.PhaseOptions)

	if tr.opts.sourceInfo {
		res.FileDescriptorProto().SourceCodeInfo = sourceinfo.GenerateSourceInfo(res.AST(), optsIndex)
		linked.PopulateSourceCodeInfo()
		lap(metrics.PhaseSourceInfo)
	}
	linked.RemoveAST()

	tr.done[path] = linked
	tr.perFile[path] = m
	return linked, nil
}

func (tr *tracer) read(path string) ([]byte, error) {
	for _, dir := range tr.opts.importPaths {
		data, err := os.ReadFile(filepath.Join(dir, path))
		if err == nil {
			return data, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%s: not found in import paths", path)
}

// stopwatch returns a func that attributes the time since its previous call
// to the given phase ("" = discard, e.g. time spent loading imports).
func stopwatch(m *metrics.File) func(phase string) {
	last := time.Now()
	return func(phase string) {
		now := time.Now()
		if phase != "" {
			ns := now.Sub(last).Nanoseconds()
			m.Phases[phase] += ns
			m.TotalNs += ns
		}
		last = now
	}
}
