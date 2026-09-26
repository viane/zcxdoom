// Package repopath finds the repository root from any working directory,
// so tools under tools/ behave the same whether run via `go run ./tools/x`
// from the repo root or as a built binary invoked from elsewhere.
package repopath

import (
	"fmt"
	"os"
	"path/filepath"
)

// Root walks up from the current directory until it finds the Dockerfile
// that marks the repository root.
func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no Dockerfile found above %s", dir)
		}
		dir = parent
	}
}
