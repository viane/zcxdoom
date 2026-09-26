// Package vendortag names the local Docker tags vendored base images are
// saved and loaded under, shared between tools/vendorimages (which writes
// vendor/images/) and tools/offlinebuild (which loads it back).
package vendortag

import "fmt"

// For returns the canonical local tag for a vendored image, e.g.
// "zcxdoom-vendor/ubuntu-20.04:arm64".
func For(name, arch string) string {
	return fmt.Sprintf("zcxdoom-vendor/%s:%s", name, arch)
}
