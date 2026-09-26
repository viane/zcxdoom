// Command offlinebuild builds the zcxdoom image using only the base
// images already vendored into vendor/images/ -- no Docker Hub access
// required, so a build never fails to Docker Hub's anonymous pull rate
// limit. It still needs network access to Ubuntu's own package archives
// (apt-get, inside the Dockerfile) to install build and runtime
// dependencies; that is a different, much less restrictive service and
// is not part of what this vendors.
//
// It works by `docker load`-ing the vendored tarball(s) for the requested
// architecture under a fixed local tag, then invoking
// `docker buildx build` with --build-arg pointing the Dockerfile's FROM
// lines at those local tags instead of their Docker Hub names.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"

	"zcxdoom/tools/internal/repopath"
	"zcxdoom/tools/internal/vendortag"
)

var vendoredImages = []string{"ubuntu-20.04", "golang-1.17-alpine"}

func main() {
	arch := flag.String("arch", defaultArch(), "target architecture: amd64, arm64, or s390x")
	tag := flag.String("tag", "zcxdoom:latest", "tag to give the built image")
	flag.Parse()

	repoRoot, err := repopath.Root()
	if err != nil {
		fatal(err)
	}

	buildArgs := []string{
		"buildx", "build",
		"--platform", "linux/" + *arch,
		"--load",
		"-t", *tag,
	}
	for _, name := range vendoredImages {
		loadedTag, err := loadVendored(repoRoot, name, *arch)
		if err != nil {
			fatal(fmt.Errorf("loading vendored %s for %s: %w", name, *arch, err))
		}
		fmt.Printf("loaded %s\n", loadedTag)
		buildArgs = append(buildArgs, "--build-arg", buildArgName(name)+"="+loadedTag)
	}
	buildArgs = append(buildArgs, ".")

	fmt.Printf("running: docker %v\n", buildArgs)
	cmd := exec.Command("docker", buildArgs...)
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fatal(fmt.Errorf("docker buildx build: %w", err))
	}
}

// buildArgName maps a vendored image name to the Dockerfile ARG that
// selects it. Kept as one small mapping rather than baking Dockerfile
// knowledge into the vendored file names themselves.
func buildArgName(name string) string {
	switch name {
	case "ubuntu-20.04":
		return "UBUNTU_IMAGE"
	case "golang-1.17-alpine":
		return "GOLANG_IMAGE"
	default:
		fatal(fmt.Errorf("no Dockerfile ARG known for vendored image %q", name))
		return ""
	}
}

// loadVendored feeds vendor/images/<name>-<arch>.tar.gz (or its .part-NNN
// chunks, concatenated in order) into `docker load` and returns the tag
// it was loaded under. docker load auto-detects gzip, so the compressed
// bytes are streamed straight through with no local decompression step.
func loadVendored(repoRoot, name, arch string) (string, error) {
	base := filepath.Join(repoRoot, "vendor", "images", fmt.Sprintf("%s-%s.tar.gz", name, arch))
	parts, err := filepath.Glob(base + ".part-*")
	if err != nil {
		return "", err
	}
	sort.Strings(parts)

	var sources []string
	if len(parts) > 0 {
		sources = parts
	} else if _, err := os.Stat(base); err == nil {
		sources = []string{base}
	} else {
		return "", fmt.Errorf("no vendored image found at %s (or .part-* chunks); run tools/vendorimages first", base)
	}

	cmd := exec.Command("docker", "load")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}

	copyErr := make(chan error, 1)
	go func() {
		defer stdin.Close()
		for _, src := range sources {
			f, err := os.Open(src)
			if err != nil {
				copyErr <- err
				return
			}
			_, err = io.Copy(stdin, f)
			f.Close()
			if err != nil {
				copyErr <- err
				return
			}
		}
		copyErr <- nil
	}()

	waitErr := cmd.Wait()
	if err := <-copyErr; err != nil {
		return "", fmt.Errorf("streaming %s into docker load: %w", base, err)
	}
	if waitErr != nil {
		return "", fmt.Errorf("docker load: %w", waitErr)
	}

	return vendortag.For(name, arch), nil
}

// defaultArch maps the host's Go architecture name to Docker's naming, so
// running with no -arch flag targets the machine you're on.
func defaultArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return "arm64"
	case "s390x":
		return "s390x"
	default:
		return "amd64"
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
