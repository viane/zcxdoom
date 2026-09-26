# zCX DOOM

Classic DOOM, running headless in a container and playable over VNC.

This is a fork of [storax/kubedoom](https://github.com/storax/kubedoom) (which
let you kill Kubernetes pods by shooting them in Doom) with all of the
Kubernetes integration removed: no `kubectl`, no pod/namespace killing, no
cluster RBAC. What's left is just the container plumbing to run DOOM headless
and expose it over VNC.

kubedoom itself is a fork of the excellent
[gideonred/dockerdoomd](https://github.com/gideonred/dockerdoomd) using a
slightly modified Doom, forked from https://github.com/gideonred/dockerdoom,
which was forked from psdoom.

![DOOM](assets/doom.jpg)

## Running Locally

In order to run locally you will need to

1. Run the zcxdoom container
2. Attach a VNC client to the appropriate port (5901)

### With Docker

```console
$ docker run -p5901:5900 --rm -it --name zcxdoom \
  ghcr.io/viane/zcxdoom:latest
```

### With Podman

```console
$ podman run -it -p5901:5900/tcp --name zcxdoom \
  ghcr.io/viane/zcxdoom:latest
```

### Attaching a VNC Client

Now start a VNC viewer and connect to `localhost:5901`. The password is `idbehold`:
```console
$ vncviewer viewer localhost:5901
```
You should now see DOOM! It starts on E1M1 at skill 1, keyboard-only (the
mouse is disabled). Pause the game with `ESC`.

## Building zcxdoom

### Offline (recommended): from this repo's vendored base images

```console
$ go run ./tools/offlinebuild -arch amd64 -tag zcxdoom:latest
```

This needs no Docker Hub access at all. `ubuntu:20.04` and `golang:1.17-alpine`
are vendored in this repository under `vendor/images/` (one copy per supported
architecture), and this command loads them locally instead of pulling from a
registry -- so a build can't fail to Docker Hub's anonymous pull rate limit,
which is easy to hit from a shared IP (a CI runner, a corporate NAT) and is
exactly what motivated vendoring them. `-arch` accepts `amd64`, `arm64`, or
`s390x` (see [Multi-architecture](#multi-architecture) below); it defaults to
your machine's own architecture.

The Dockerfile's `apt-get install` steps (the compiler toolchain and the SDL/
VNC runtime libraries) still reach out to Ubuntu's own package archives, which
is a different, much less restrictive service than Docker Hub and isn't part
of what this vendors. A fully airgapped build with no network access at all
would need those mirrored too; see `tools/README.md` for the size trade-off
that decision was left out of scope on purpose.

### Online: a plain docker build

If Docker Hub is reachable, the Dockerfile also works completely normally on
its own, unchanged from any other project:

```console
$ docker build -t zcxdoom .
```

Both paths accept `--build-arg VNCPASSWORD=differentpw` to change the VNC
password from its default (`idbehold`).

### The WAD file

The DOOM IWAD (`assets/doom1.wad`) is bundled directly in this repository, so
building the image needs no network access to a third-party WAD mirror. It is
the free/open [Freedoom](https://github.com/freedoom/freedoom) `freedoom1.wad`,
not id Software's shareware data; see `assets/doom1.wad.LICENSE.txt` for its
license. If you'd rather ship id Software's original shareware `doom1.wad`,
drop your own copy at that same path before building.

### Multi-architecture

zcxdoom builds and runs correctly on `amd64`, `arm64`, and `s390x` (yes,
really -- including on that last one, which is big-endian; the renderer has
no endianness bugs). Cross-building for an architecture other than your
machine's needs QEMU user-mode emulation registered (`docker run --privileged
--rm tonistiigi/binfmt --install all`; already set up for you on GitHub
Actions by `docker/setup-qemu-action`), since Docker itself has no way to run
another architecture's binaries otherwise:

```console
$ go run ./tools/offlinebuild -arch s390x -tag zcxdoom:s390x
```

The published `ghcr.io/viane/zcxdoom` image is a single multi-arch manifest
covering all three; `docker run` picks the right one for your machine
automatically.

## Testing and controlling it over VNC

`tools/` has a small, dependency-free Go VNC client (screenshot + key
input) used for a smoke test and, eventually, for AI/computer-use control.
See [`tools/README.md`](tools/README.md).
