// Command vendorimages is a maintainer tool: it fetches this project's two
// Docker Hub base images (ubuntu:20.04, golang:1.17-alpine) for each
// supported architecture, and writes them into vendor/images/ as gzipped
// `docker save` tarballs, splitting anything over GitHub's 100MB per-file
// limit into numbered chunks.
//
// It exists so that building zcxdoom never depends on Docker Hub being
// reachable (Docker Hub's anonymous pull rate limit is easy to hit,
// especially from a shared or CI IP) -- only on the vendored files in this
// repository, loaded by ../offlinebuild. Package installation
// (apt-get, inside the Dockerfile) still needs network access to Ubuntu's
// own package archives; those aren't part of this problem and aren't
// vendored.
//
// Run it only when adding an architecture or intentionally updating a
// base image (e.g. for a security patch) -- it needs `docker` and network
// access to pull from Docker Hub. Everyday builds use the already-vendored
// files via `go run ./tools/offlinebuild`, which needs neither.
package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"zcxdoom/tools/internal/repopath"
	"zcxdoom/tools/internal/vendortag"
)

// splitLimit is comfortably under GitHub's 100MB hard per-file cap.
const splitLimit = 90 * 1024 * 1024

type baseImage struct {
	// name becomes both the vendored file's prefix and the local tag's
	// repository (zcxdoom-vendor/<name>:<arch>).
	name     string
	upstream string // Docker Hub repository, e.g. "ubuntu"
	// digests pins the exact manifest per architecture, so vendoring is
	// reproducible and always matches what was actually tested -- not
	// whatever a moving tag happens to resolve to on the day this runs.
	digests map[string]string
}

var images = []baseImage{
	{
		name:     "ubuntu-20.04",
		upstream: "ubuntu",
		digests: map[string]string{
			"amd64": "sha256:c664f8f86ed5a386b0a340d981b8f81714e21a8b9c73f658c4bea56aa179d54a",
			"arm64": "sha256:722ea796ac2d57eeb3627c58a582fc1acc58be51faf815e1bce1682ae5c092f7",
			"s390x": "sha256:c1661b84ff68805c464d0d641c1f183776c0e663162fbbbc1ee731f3525ecf0d",
		},
	},
	{
		name:     "golang-1.17-alpine",
		upstream: "golang",
		digests: map[string]string{
			"amd64": "sha256:c80567372be0d486766593cc722d3401038e2f150a0f6c5c719caa63afb4026a",
			"arm64": "sha256:544ba2d3845507b8e23ed02f9e5bb301dc527dd16a8fb690c3ed2ec183141683",
			"s390x": "sha256:5d3a9eda4e7ae788fd1e6ad1639135ca47e08ae72a3a24dabff0b4ab1a318f0d",
		},
	},
}

func main() {
	archFlag := flag.String("arch", "amd64,arm64,s390x", "comma-separated architectures to vendor")
	flag.Parse()
	arches := splitCSV(*archFlag)

	repoRoot, err := repopath.Root()
	if err != nil {
		fatal(err)
	}
	outDir := filepath.Join(repoRoot, "vendor", "images")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatal(err)
	}

	for _, img := range images {
		for _, arch := range arches {
			digest, ok := img.digests[arch]
			if !ok {
				fatal(fmt.Errorf("%s: no digest recorded for arch %q", img.name, arch))
			}
			if err := vendorOne(outDir, img, arch, digest); err != nil {
				fatal(fmt.Errorf("%s (%s): %w", img.name, arch, err))
			}
		}
	}
}

func vendorOne(outDir string, img baseImage, arch, digest string) error {
	ref := img.upstream + "@" + digest
	fmt.Printf("pulling %s (%s)...\n", ref, arch)
	if out, err := exec.Command("docker", "pull", ref).CombinedOutput(); err != nil {
		return fmt.Errorf("docker pull: %w\n%s", err, out)
	}

	tag := vendortag.For(img.name, arch)
	if out, err := exec.Command("docker", "tag", ref, tag).CombinedOutput(); err != nil {
		return fmt.Errorf("docker tag: %w\n%s", err, out)
	}

	outPath := filepath.Join(outDir, fmt.Sprintf("%s-%s.tar.gz", img.name, arch))
	if err := saveCompressed(tag, outPath); err != nil {
		return fmt.Errorf("saving %s: %w", outPath, err)
	}
	fmt.Printf("  wrote %s\n", outPath)

	return splitIfNeeded(outPath, splitLimit)
}

// saveCompressed runs `docker save` and gzips its output directly to
// outPath, so the large uncompressed tar never touches disk.
func saveCompressed(tag, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		return err
	}

	cmd := exec.Command("docker", "save", tag)
	cmd.Stdout = gz
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return gz.Close()
}

// splitIfNeeded breaks a file into <path>.part-000, .part-001, ... chunks
// of at most limit bytes each, removing the original, if and only if the
// original exceeds limit. Small files (like the Ubuntu base image) are
// left as a single file.
func splitIfNeeded(path string, limit int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= limit {
		return nil
	}

	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()

	buf := make([]byte, limit)
	for i := 0; ; i++ {
		n, readErr := io.ReadFull(in, buf)
		if n > 0 {
			partPath := fmt.Sprintf("%s.part-%03d", path, i)
			if err := os.WriteFile(partPath, buf[:n], 0o644); err != nil {
				return err
			}
			fmt.Printf("  split: %s (%d bytes)\n", partPath, n)
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	in.Close()
	return os.Remove(path)
}

func splitCSV(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	sort.Strings(out)
	return out
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
