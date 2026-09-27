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

The VNC server runs with `-shared`, so this isn't limited to one viewer at
a time: a second VNC client, a recording, a browser-based viewer, or
`aiplay` (see below) can all connect at once without kicking each other
off.

## Building zcxdoom

**All you need is Docker.** Nothing here requires Go, or any other tool, on
your machine -- Go is only used to *produce* the files this reads, not to
consume them.

### Online: a plain docker build

If Docker Hub is reachable, this is unchanged from any other project:

```console
$ docker build -t zcxdoom .
```

### Offline: from this repo's vendored base images

`ubuntu:20.04` and `golang:1.17-alpine` are vendored in this repository under
`vendor/images/` (one copy per supported architecture), because Docker Hub's
anonymous pull rate limit is easy to hit from a shared IP (a CI runner, a
corporate NAT) and will otherwise fail your build outright. To build from
them instead of pulling:

```console
$ cat vendor/images/golang-1.17-alpine-amd64.tar.gz.part-* | docker load
$ docker load -i vendor/images/ubuntu-20.04-amd64.tar.gz
$ docker build \
    --build-arg UBUNTU_IMAGE=zcxdoom-vendor/ubuntu-20.04:amd64 \
    --build-arg GOLANG_IMAGE=zcxdoom-vendor/golang-1.17-alpine:amd64 \
    -t zcxdoom .
```

(Swap `amd64` for `arm64` or `s390x` to match your machine -- see
[Multi-architecture](#multi-architecture).) The first two lines just load the
already-downloaded images into Docker's local store under fixed names instead
of fetching them from a registry; `docker build` then picks them up by name
like any other base image, and BuildKit never attempts a registry round-trip
for them at all. If you'd rather not run three commands, `go run
./tools/offlinebuild -arch amd64` does exactly this in one step and picks your
architecture automatically -- convenient if you already have Go, but strictly
optional.

The Dockerfile's `apt-get install` steps (the compiler toolchain and the SDL/
VNC runtime libraries) still reach out to Ubuntu's own package archives, which
is a different, much less restrictive service than Docker Hub and isn't part
of what this vendors. A fully airgapped build with no network access at all
would need those mirrored too; see `tools/README.md` for the size trade-off
that decision was left out of scope on purpose.

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
no endianness bugs). The published `ghcr.io/viane/zcxdoom` image is a single
multi-arch manifest covering all three, so plain `docker run
ghcr.io/viane/zcxdoom:latest` already gets you the right one for your
machine automatically, and **building for your own machine's architecture
needs nothing beyond what's already described above.**

The only case that needs anything extra is *cross*-building: producing, say,
an `s390x` image on an `amd64` machine, which requires Docker to be able to
run another architecture's binaries via QEMU emulation. Docker Desktop (Mac
and Windows) already includes this. On Linux, if `docker build --platform
linux/s390x ...` fails with an "exec format error", your Docker Engine
install needs it registered once; see
[Docker's own multi-platform build documentation](https://docs.docker.com/build/building/multi-platform/)
for the current recommended way to do that on your distribution -- this
project doesn't need a specific method and doesn't require running anything
`--privileged` itself. Once emulation is set up, `-arch` just becomes a flag:

```console
$ go run ./tools/offlinebuild -arch s390x -tag zcxdoom:s390x
```

## Testing and controlling it over VNC

`tools/` has a small, dependency-free Go VNC client (screenshot + key
input, plus recording to video and a browser-based viewer) used for a
smoke test and for driving the game by hand or from a script. See
[`tools/README.md`](tools/README.md).

## AI computer-use PoC

[`aiplay/`](aiplay/README.md) is a proof of concept that plays zcxdoom the
same way a human would: over VNC, with no access to the game's internals.
A fast "System 1" loop decides the next key press many times a second,
while a slower "System 2" call (a real multimodal LLM) periodically looks
at the screen and sets overall tactics. See its README for the
architecture and how to run it.
