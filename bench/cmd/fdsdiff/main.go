// Command fdsdiff compares two FileDescriptorSet files semantically.
//
// Files are matched by name (order inside the set is ignored), then compared
// with proto.Equal one file at a time so peak memory stays low even on
// 7,400+ file corpora with SourceCodeInfo. Custom options (extensions) are
// decoded using only the subset of files in the expected set that define
// extensions (and their transitive imports), keeping memory usage minimal.
// On mismatch a cmp diff of the first differing files is printed.
// Exit code: 0 equal, 1 different, 2 usage/IO error.
//
//	fdsdiff [-raw] [-no-source-info] [-max-diffs N] expected.pb actual.pb
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"sort"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type fileDiff struct {
	name string
	want *descriptorpb.FileDescriptorProto
	got  *descriptorpb.FileDescriptorProto
}

func main() {
	debug.SetGCPercent(20)
	debug.SetMemoryLimit(500 * 1024 * 1024)
	raw := flag.Bool("raw", false, "compare custom option bytes verbatim instead of decoding them")
	noSourceInfo := flag.Bool("no-source-info", false, "strip source_code_info before comparing")
	maxDiffs := flag.Int("max-diffs", 3, "max number of per-file diffs to print")
	maxDiffLen := flag.Int("max-diff-len", 4000, "truncate each printed diff to this many bytes")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: fdsdiff [flags] expected.pb actual.pb")
		os.Exit(2)
	}

	wantData := read(flag.Arg(0))
	wantRaw := splitSet(flag.Arg(0), wantData)

	gotData := read(flag.Arg(1))
	gotRaw := splitSet(flag.Arg(1), gotData)

	var opts proto.UnmarshalOptions
	resolverBuilt := false
	ensureResolver := func() {
		if !resolverBuilt && !*raw {
			resolverBuilt = true
			opts.Resolver = extensionResolverFromRaw(flag.Arg(0), wantRaw)
			runtime.GC()
			debug.FreeOSMemory()
		}
	}

	names := make(map[string]bool, len(wantRaw)+len(gotRaw))
	for n := range wantRaw {
		names[n] = true
	}
	for n := range gotRaw {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	var missing, extra, different []string
	var savedDiffs []fileDiff
	unmarshaledCount := 0

	for _, n := range sorted {
		wBytes, okW := wantRaw[n]
		gBytes, okG := gotRaw[n]
		switch {
		case !okG:
			missing = append(missing, n)
		case !okW:
			extra = append(extra, n)
		default:
			if *noSourceInfo {
				wBytes = stripSourceCodeInfoWire(wBytes)
				gBytes = stripSourceCodeInfoWire(gBytes)
			}
			if bytes.Equal(wBytes, gBytes) {
				continue
			}
			ensureResolver()
			w := unmarshalOne(flag.Arg(0), n, wBytes, opts, *noSourceInfo)
			g := unmarshalOne(flag.Arg(1), n, gBytes, opts, *noSourceInfo)
			if !proto.Equal(w, g) {
				different = append(different, n)
				if len(savedDiffs) < *maxDiffs {
					savedDiffs = append(savedDiffs, fileDiff{name: n, want: w, got: g})
				}
			}
			unmarshaledCount++
			if unmarshaledCount%50 == 0 {
				runtime.GC()
			}
		}
	}

	fmt.Printf("files: expected=%d actual=%d missing=%d extra=%d different=%d\n",
		len(wantRaw), len(gotRaw), len(missing), len(extra), len(different))
	if len(missing)+len(extra)+len(different) == 0 {
		fmt.Println("OK: descriptor sets are equivalent")
		return
	}
	printList("missing", missing)
	printList("extra", extra)
	printList("different", different)
	for _, fd := range savedDiffs {
		d := cmp.Diff(fd.want, fd.got, protocmp.Transform())
		if len(d) > *maxDiffLen {
			d = d[:*maxDiffLen] + "\n... (truncated)"
		}
		fmt.Printf("\n--- diff %s (-expected +actual):\n%s\n", fd.name, d)
	}
	os.Exit(1)
}

func read(path string) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	return data
}

func splitSet(path string, data []byte) map[string][]byte {
	out := make(map[string][]byte)
	b := data
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			fmt.Fprintf(os.Stderr, "%s: invalid wire tag\n", path)
			os.Exit(2)
		}
		b = b[n:]
		if num == 1 && typ == protowire.BytesType {
			rawFile, m := protowire.ConsumeBytes(b)
			if m < 0 {
				fmt.Fprintf(os.Stderr, "%s: invalid file bytes\n", path)
				os.Exit(2)
			}
			b = b[m:]
			name := extractFileName(rawFile)
			out[name] = rawFile
		} else {
			m := protowire.ConsumeFieldValue(num, typ, b)
			if m < 0 {
				fmt.Fprintf(os.Stderr, "%s: invalid field value\n", path)
				os.Exit(2)
			}
			b = b[m:]
		}
	}
	return out
}

func extractFileName(rawFile []byte) string {
	b := rawFile
	var name string
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			break
		}
		b = b[n:]
		if num == 1 && typ == protowire.BytesType {
			v, m := protowire.ConsumeBytes(b)
			if m < 0 {
				break
			}
			name = string(v)
			b = b[m:]
		} else {
			m := protowire.ConsumeFieldValue(num, typ, b)
			if m < 0 {
				break
			}
			b = b[m:]
		}
	}
	return name
}

// wireHasExtension checks whether a raw FileDescriptorProto wire slice contains
// top-level extension (field 7) or nested message extension (field 6 inside field 4).
func wireHasExtension(rawFile []byte) bool {
	b := rawFile
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return false
		}
		b = b[n:]
		if num == 7 && typ == protowire.BytesType {
			return true
		}
		if num == 4 && typ == protowire.BytesType {
			msgBytes, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return false
			}
			if msgWireHasExtension(msgBytes) {
				return true
			}
			b = b[m:]
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			return false
		}
		b = b[m:]
	}
	return false
}

func msgWireHasExtension(msgBytes []byte) bool {
	b := msgBytes
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return false
		}
		b = b[n:]
		if num == 6 && typ == protowire.BytesType {
			return true
		}
		if num == 3 && typ == protowire.BytesType {
			nested, m := protowire.ConsumeBytes(b)
			if m < 0 {
				return false
			}
			if msgWireHasExtension(nested) {
				return true
			}
			b = b[m:]
			continue
		}
		m := protowire.ConsumeFieldValue(num, typ, b)
		if m < 0 {
			return false
		}
		b = b[m:]
	}
	return false
}

func stripSourceCodeInfoWire(rawFile []byte) []byte {
	out := make([]byte, 0, len(rawFile))
	b := rawFile
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return rawFile
		}
		m := protowire.ConsumeFieldValue(num, typ, b[n:])
		if m < 0 {
			return rawFile
		}
		total := n + m
		if num != 9 {
			out = append(out, b[:total]...)
		}
		b = b[total:]
	}
	return out
}

// extensionResolverFromRaw builds a type registry using ONLY the files that
// define extensions (plus their transitive dependencies), reducing memory on
// googleapis from >1.2 GB (7,427 files) to <15 MB (~60 files).
func extensionResolverFromRaw(path string, rawFiles map[string][]byte) *dynamicpb.Types {
	parsed := make(map[string]*descriptorpb.FileDescriptorProto)
	var loadWithDeps func(name string)
	loadWithDeps = func(name string) {
		if _, ok := parsed[name]; ok {
			return
		}
		raw, ok := rawFiles[name]
		if !ok {
			return
		}
		stripped := stripSourceCodeInfoWire(raw)
		f := new(descriptorpb.FileDescriptorProto)
		if err := proto.Unmarshal(stripped, f); err != nil {
			return
		}
		f.Service = nil
		parsed[name] = f
		for _, dep := range f.Dependency {
			loadWithDeps(dep)
		}
	}

	for name, raw := range rawFiles {
		if wireHasExtension(raw) {
			loadWithDeps(name)
		}
	}

	var set descriptorpb.FileDescriptorSet
	set.File = make([]*descriptorpb.FileDescriptorProto, 0, len(parsed))
	for _, f := range parsed {
		set.File = append(set.File, f)
	}
	files, err := protodesc.NewFiles(&set)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot decode custom options (%v); comparing raw bytes\n", err)
		return nil
	}
	return dynamicpb.NewTypes(files)
}

func unmarshalOne(path, name string, raw []byte, opts proto.UnmarshalOptions, noSourceInfo bool) *descriptorpb.FileDescriptorProto {
	if noSourceInfo {
		raw = stripSourceCodeInfoWire(raw)
	}
	f := new(descriptorpb.FileDescriptorProto)
	if err := opts.Unmarshal(raw, f); err != nil {
		fmt.Fprintf(os.Stderr, "%s (%s): %v\n", path, name, err)
		os.Exit(2)
	}
	if noSourceInfo {
		f.SourceCodeInfo = nil
	}
	return f
}

func printList(label string, items []string) {
	const limit = 20
	for i, n := range items {
		if i == limit {
			fmt.Printf("  %s: ... and %d more\n", label, len(items)-limit)
			return
		}
		fmt.Printf("  %s: %s\n", label, n)
	}
}
