// Command parsebench parses .proto files without resolving imports or linking,
// the way a linter reads them, with easyprotoc or with protocompile. Files are
// parsed on every core; the command fails on the first syntax error.
//
//	parsebench -impl easyprotoc -I googleapis -I include @files.args
//	parsebench -impl protocompile -I googleapis google/api/http.proto
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/bufbuild/protocompile/parser"
	"github.com/bufbuild/protocompile/reporter"

	"github.com/easyp-tech/easyprotoc/protoc/syntax"
)

func main() {
	impl := flag.String("impl", "", "easyprotoc or protocompile")
	var roots importPaths
	flag.Var(&roots, "I", "directory the file paths are relative to; may repeat, the first that has the file wins")
	flag.Parse()
	if len(roots) == 0 {
		roots = importPaths{"."}
	}

	var parse func(path string, src []byte) error
	switch *impl {
	case "easyprotoc":
		parse = func(path string, src []byte) error {
			_, err := syntax.Parse(path, src)
			return err
		}
	case "protocompile":
		parse = func(path string, src []byte) error {
			_, err := parser.Parse(path, bytes.NewReader(src), reporter.NewHandler(nil))
			return err
		}
	default:
		fatal(fmt.Errorf("-impl must be easyprotoc or protocompile, got %q", *impl))
	}

	files, err := expandArgs(flag.Args())
	if err != nil {
		fatal(err)
	}
	if len(files) == 0 {
		fatal(fmt.Errorf("no input files"))
	}

	var next atomic.Int64
	var failed atomic.Bool
	var firstErr error
	var once sync.Once
	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !failed.Load() {
				i := int(next.Add(1) - 1)
				if i >= len(files) {
					return
				}
				src, err := roots.read(files[i])
				if err == nil {
					err = parse(files[i], src)
				}
				if err != nil {
					once.Do(func() { firstErr = err })
					failed.Store(true)
				}
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		fatal(firstErr)
	}
}

type importPaths []string

func (p *importPaths) String() string { return strings.Join(*p, ",") }

func (p *importPaths) Set(dir string) error {
	*p = append(*p, dir)
	return nil
}

func (p importPaths) read(path string) ([]byte, error) {
	var err error
	for _, dir := range p {
		var src []byte
		if src, err = os.ReadFile(filepath.Join(dir, path)); err == nil {
			return src, nil
		}
	}
	return nil, err
}

// expandArgs replaces every @file argument with the lines of that file.
func expandArgs(args []string) ([]string, error) {
	var files []string
	for _, arg := range args {
		if !strings.HasPrefix(arg, "@") {
			files = append(files, arg)
			continue
		}
		f, err := os.Open(arg[1:])
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if line := strings.TrimSpace(sc.Text()); line != "" {
				files = append(files, line)
			}
		}
		err = sc.Err()
		_ = f.Close()
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "parsebench:", err)
	os.Exit(1)
}
