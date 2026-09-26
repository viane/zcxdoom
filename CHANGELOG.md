# 0.7.0
* Removed all Kubernetes integration: no more `kubectl`, pod/namespace killing,
  `-mode` flag, or `NAMESPACE` env var. The container now runs plain classic
  DOOM with nothing else attached.
* Removed the `manifest/` Kubernetes deployment manifests and `kind-config.yaml`
  (no longer applicable, there is nothing Kubernetes-specific left to deploy).
* The DOOM IWAD is now bundled directly in the repo at `assets/doom1.wad`
  (the free/open Freedoom WAD) instead of being downloaded from a third-party
  mirror at build time, removing a network dependency from the build.
* `psdoom` is now started with `-nopsmon`, so it never spawns "process monster"
  enemies or talks to any socket.
* Renamed the Go module and built binary from `kubedoom` to `zcxdoom`
  (`kubedoom.go` is now `main.go`) to drop the leftover Kubernetes branding.

# 0.6.0
* New image ghcr.io/storax/kubedoom:0.6.0
* Latest image available as ghcr.io/storax/kubedoom:latest.
* Add support for building on different architectures.
* Update kubernetes to 1.23.2
* Update to Ubuntu 21.10
* Github Actions for building the image.
* VNC password can be configured during build via the `VNCPASSWORD` build argument.

# 0.5.0

* New image storaxdev/kubedoom:1.0.0
* New default VNC password is `idbehold`. 
* Update kubernetes to 1.19.1
* Update to Ubuntu 20.10

# 0.4.0

* New image storadev/kubedoom:0.4.0
* New `-mode` flag to switch between killing pods or namespaces.
* Update kubernetes to 1.18.2

# 0.3.0

* New image storadev/kubedoom:0.3.0
* Update kubernetes to 1.18.1

# 0.2.0

* New image storadev/kubedoom:0.2.0
* Update kubernetes to 1.17.3

# 0.1.0

* Initial release
