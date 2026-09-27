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
* Added `tools/`: a dependency-free Go VNC client (`tools/rfb` +
  `tools/vncharness` CLI) and a smoke test (`tools/smoketest`) that checks
  a running instance actually renders and responds to input, whether it's
  local, self-managed via Docker, or a remote deployment.
* Added multi-architecture support for `arm64` and `s390x` alongside
  `amd64` (verified end to end under QEMU emulation, including that
  rendering is correct on the big-endian `s390x`).
* Vendored this project's Docker Hub base images (`ubuntu:20.04`,
  `golang:1.17-alpine`) into `vendor/images/`, one copy per architecture,
  and added `tools/offlinebuild` to build from them directly. A build no
  longer needs Docker Hub to be reachable at all, and so can't fail to its
  anonymous pull rate limit; `tools/vendorimages` regenerates the vendored
  files when a base image needs updating. The Dockerfile's `FROM` lines
  still default to their normal Docker Hub names for plain `docker build`.
* CI (`container.yaml`) now builds and publishes `linux/s390x` alongside
  `amd64`/`arm64`, via the same vendored-image path as local builds.
* Building offline needs only Docker (`docker load` the vendored files,
  then `docker build --build-arg ...`; documented in the main README).
  `tools/offlinebuild` remains available as an optional one-command
  shortcut for anyone who already has Go, never a requirement.
* Removed the `--privileged` QEMU/binfmt command from the README's main
  flow -- it was never needed to build or run zcxdoom for your own
  machine's architecture, only for the (rare) case of cross-building for
  a different one, and even then Docker Desktop already includes it.
* Replaced `assets/doom.jpg` with a current screenshot; the old one
  predated removing Kubernetes integration and still showed pods
  rendered as monster name tags (`kube-system`, `kube-proxy-...`).
* x11vnc now runs with `-shared`, so more than one VNC client (a human
  viewer, a recording, `aiplay`, the browser-based viewer) can connect to
  a running instance at the same time without kicking each other off.
* Added a `record` subcommand to `tools/vncharness`: captures a live VNC
  session to an MP4 (via `ffmpeg`, if it's on `PATH`) or a plain PNG
  sequence otherwise.
* Added `tools/webvnc`, an optional sidecar image (noVNC + websockify) for
  watching a running instance in a plain web browser. Kept separate from
  the core game image since it needs network access at build time, unlike
  the core image's offline, vendored build path.
* Added `aiplay`, a computer-use proof of concept that plays zcxdoom over
  VNC: a fast "System 1" loop (a Kev/Jev-compatible typed-decision model,
  self-hosted or hosted) picks the next key press many times a second,
  while a slower "System 2" call (Gemini, by default) periodically looks
  at the screen and sets overall tactics. It's a separate Go module (its
  own `go.mod`, tied in via `go.work`) with no reverse dependency from the
  game -- see `aiplay/README.md`.

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
