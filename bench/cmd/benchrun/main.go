// Command benchrun measures external commands (wall time, CPU time, peak RSS)
// and renders comparison tables. It is a dependency-free stand-in for
// hyperfine that also records memory and machine info.
//
//	benchrun run -name protoc -corpus googleapis -n 10 -warmup 2 -out results.jsonl -- protoc ...
//	benchrun report -baseline protoc results.jsonl
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

type sample struct {
	WallNs   int64 `json:"wall_ns"`
	UserNs   int64 `json:"user_ns"`
	SysNs    int64 `json:"sys_ns"`
	MaxRSSKB int64 `json:"max_rss_kb"`
}

type result struct {
	Name     string    `json:"name"`
	Corpus   string    `json:"corpus"`
	Label    string    `json:"label,omitempty"`
	Command  []string  `json:"command"`
	Time     time.Time `json:"time"`
	Machine  machine   `json:"machine"`
	Samples  []sample  `json:"samples"`
	MinNs    int64     `json:"min_ns"`
	MedianNs int64     `json:"median_ns"`
	MeanNs   int64     `json:"mean_ns"`
	StddevNs int64     `json:"stddev_ns"`
	MaxRSSKB int64     `json:"max_rss_kb"`
}

type machine struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	CPUs   int    `json:"cpus"`
	CPU    string `json:"cpu,omitempty"`
	GitRev string `json:"git_rev,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "run":
		run(os.Args[2:])
	case "report":
		report(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: benchrun run [flags] -- cmd args... | benchrun report [flags] results.jsonl")
	os.Exit(2)
}

func run(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	name := fs.String("name", "", "implementation name (e.g. protoc, protocompile, easyp)")
	corpus := fs.String("corpus", "", "corpus name")
	label := fs.String("label", "", "free-form label, e.g. experiment id")
	n := fs.Int("n", 10, "measured runs")
	warmup := fs.Int("warmup", 2, "warmup runs")
	out := fs.String("out", "", "append JSON line to this file")
	_ = fs.Parse(args)
	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 || *name == "" {
		fmt.Fprintln(os.Stderr, "run: -name and a command are required")
		os.Exit(2)
	}

	for range *warmup {
		if _, err := once(cmdArgs); err != nil {
			fmt.Fprintf(os.Stderr, "%s: warmup failed: %v\n", *name, err)
			os.Exit(1)
		}
	}
	res := result{
		Name: *name, Corpus: *corpus, Label: *label, Command: cmdArgs,
		Time: time.Now().UTC(), Machine: machineInfo(),
	}
	for range *n {
		s, err := once(cmdArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: run failed: %v\n", *name, err)
			os.Exit(1)
		}
		res.Samples = append(res.Samples, s)
	}
	summarize(&res)

	fmt.Printf("%-24s %-14s median %9s  min %9s  ±%8s  rss %6.0f MiB\n",
		res.Name, res.Corpus, dur(res.MedianNs), dur(res.MinNs), dur(res.StddevNs), float64(res.MaxRSSKB)/1024)
	if *out != "" {
		f, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		_ = json.NewEncoder(f).Encode(res)
	}
}

// once runs the command one time. Its output is dropped (compilers print
// warnings on every run) and shown only when the command fails.
func once(args []string) (sample, error) {
	cmd := exec.Command(args[0], args[1:]...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	start := time.Now()
	err := cmd.Run()
	wall := time.Since(start)
	if err != nil {
		return sample{}, fmt.Errorf("%w\n%s", err, output.Bytes())
	}
	s := sample{
		WallNs: wall.Nanoseconds(),
		UserNs: cmd.ProcessState.UserTime().Nanoseconds(),
		SysNs:  cmd.ProcessState.SystemTime().Nanoseconds(),
	}
	if ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
		s.MaxRSSKB = int64(ru.Maxrss)
		if runtime.GOOS == "darwin" { // bytes on macOS, KiB on Linux
			s.MaxRSSKB /= 1024
		}
	}
	return s, nil
}

func summarize(r *result) {
	walls := make([]int64, len(r.Samples))
	var sum float64
	for i, s := range r.Samples {
		walls[i] = s.WallNs
		sum += float64(s.WallNs)
		r.MaxRSSKB = max(r.MaxRSSKB, s.MaxRSSKB)
	}
	sort.Slice(walls, func(i, j int) bool { return walls[i] < walls[j] })
	mean := sum / float64(len(walls))
	var sq float64
	for _, w := range walls {
		sq += (float64(w) - mean) * (float64(w) - mean)
	}
	r.MinNs = walls[0]
	r.MedianNs = walls[len(walls)/2]
	if len(walls)%2 == 0 {
		r.MedianNs = (walls[len(walls)/2-1] + walls[len(walls)/2]) / 2
	}
	r.MeanNs = int64(mean)
	r.StddevNs = int64(math.Sqrt(sq / float64(len(walls))))
}

func report(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	baseline := fs.String("baseline", "protoc", "implementation used as 1.00x")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		usage()
	}
	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	// Latest result wins per (corpus, name, label).
	latest := map[string]result{}
	var order []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		var r result
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		key := r.Corpus + "\x00" + r.Name + "\x00" + r.Label
		if _, ok := latest[key]; !ok {
			order = append(order, key)
		}
		latest[key] = r
	}

	byCorpus := map[string][]result{}
	var corpora []string
	for _, k := range order {
		r := latest[k]
		if _, ok := byCorpus[r.Corpus]; !ok {
			corpora = append(corpora, r.Corpus)
		}
		byCorpus[r.Corpus] = append(byCorpus[r.Corpus], r)
	}
	for _, c := range corpora {
		rs := byCorpus[c]
		var base int64
		for _, r := range rs {
			if r.Name == *baseline {
				base = r.MedianNs
			}
		}
		sort.SliceStable(rs, func(i, j int) bool { return rs[i].MedianNs < rs[j].MedianNs })
		m := rs[0].Machine
		fmt.Printf("\n### %s (%s/%s, %d CPUs, %s)\n\n", c, m.OS, m.Arch, m.CPUs, m.CPU)
		fmt.Println("| impl | median | min | ±stddev | user+sys | max RSS | vs " + *baseline + " |")
		fmt.Println("|---|---:|---:|---:|---:|---:|---:|")
		for _, r := range rs {
			speed := "—"
			if base > 0 {
				speed = fmt.Sprintf("%.2fx", float64(base)/float64(r.MedianNs))
			}
			var cpu int64
			for _, s := range r.Samples {
				cpu += s.UserNs + s.SysNs
			}
			cpu /= int64(len(r.Samples))
			name := r.Name
			if r.Label != "" {
				name += " (" + r.Label + ")"
			}
			fmt.Printf("| %s | %s | %s | %s | %s | %.0f MiB | %s |\n",
				name, dur(r.MedianNs), dur(r.MinNs), dur(r.StddevNs), dur(cpu),
				float64(r.MaxRSSKB)/1024, speed)
		}
	}
}

func dur(ns int64) string {
	d := time.Duration(ns)
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.3fs", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
	default:
		return fmt.Sprintf("%.1fµs", float64(d)/float64(time.Microsecond))
	}
}

func machineInfo() machine {
	m := machine{OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU()}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
			m.CPU = strings.TrimSpace(string(out))
		}
	} else if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			k, v, ok := strings.Cut(line, ":")
			k = strings.TrimSpace(k)
			if ok && (k == "model name" || k == "Model" || k == "CPU part") {
				m.CPU = strings.TrimSpace(v)
				break
			}
		}
	}
	m.GitRev = os.Getenv("BENCH_GIT_REV")
	return m
}
