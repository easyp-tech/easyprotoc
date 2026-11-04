// Command fetch-protoc downloads the official protoc release for the current
// OS/arch, checks its sha256 against a checksums file and unpacks it
// (bin/protoc + include/) into a directory. It exists so the bench setup does
// not depend on unzip/python being installed.
//
//	fetch-protoc -version 36.2 -checksums checksums.txt -dir .cache/protoc-36.2
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	version := flag.String("version", "", "protoc version, e.g. 36.2")
	dir := flag.String("dir", "", "destination directory")
	checksums := flag.String("checksums", "", "file with \"<sha256>  <asset name>\" lines")
	flag.Parse()
	if *version == "" || *dir == "" || *checksums == "" {
		fmt.Fprintln(os.Stderr, "usage: fetch-protoc -version X.Y -checksums FILE -dir DIR")
		os.Exit(2)
	}

	platform, err := platformName()
	if err != nil {
		fatal(err)
	}
	asset := fmt.Sprintf("protoc-%s-%s.zip", *version, platform)
	want, err := lookupChecksum(*checksums, asset)
	if err != nil {
		fatal(err)
	}
	url := fmt.Sprintf("https://github.com/protocolbuffers/protobuf/releases/download/v%s/%s", *version, asset)
	fmt.Fprintf(os.Stderr, "downloading %s\n", url)
	resp, err := http.Get(url)
	if err != nil {
		fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fatal(fmt.Errorf("GET %s: %s", url, resp.Status))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		fatal(fmt.Errorf("%s: sha256 %s, want %s", asset, got, want))
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		fatal(err)
	}
	root, err := filepath.Abs(*dir)
	if err != nil {
		fatal(err)
	}
	for _, f := range zr.File {
		dst := filepath.Join(root, f.Name)
		if !strings.HasPrefix(dst, root+string(filepath.Separator)) {
			fatal(fmt.Errorf("zip entry escapes destination: %s", f.Name))
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				fatal(err)
			}
			continue
		}
		if err := extract(f, dst); err != nil {
			fatal(err)
		}
	}
	fmt.Println(filepath.Join(root, "bin", "protoc"))
}

func lookupChecksum(path, asset string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s: no checksum for %s", path, asset)
}

func extract(f *zip.File, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	mode := os.FileMode(0o644)
	if strings.HasPrefix(f.Name, "bin/") {
		mode = 0o755
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func platformName() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64":
		return "linux-x86_64", nil
	case "linux/arm64":
		return "linux-aarch_64", nil
	case "darwin/amd64":
		return "osx-x86_64", nil
	case "darwin/arm64":
		return "osx-aarch_64", nil
	case "windows/amd64":
		return "win64", nil
	}
	return "", fmt.Errorf("unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "fetch-protoc:", err)
	os.Exit(1)
}
