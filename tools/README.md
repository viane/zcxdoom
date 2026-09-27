# tools

Testing and control infrastructure for zcxdoom, built on one small,
dependency-free package:

- **`rfb`** -- a minimal RFB (VNC) protocol client. Just enough to connect,
  authenticate, take a screenshot, and send key presses. Standard library
  only, so anything built on it is a single static binary with no runtime
  dependencies -- copy it to a Windows, macOS, or Linux machine and it
  just runs.
- **`vncharness`** -- a CLI on top of `rfb`, for driving a running
  instance by hand or from a script, and for recording what it's doing to
  video.
- **`smoketest`** -- a `go test` that uses `rfb` directly to check that a
  zcxdoom instance actually boots and responds to input.

And **`webvnc`**, an optional sidecar for watching a running instance in a
browser -- see [Browser-based viewing](#browser-based-viewing-webvnc)
below.

`rfb` is also what [`aiplay`](../aiplay/README.md) is built on: a
computer-use proof of concept that plays zcxdoom over VNC the same way
everything in this directory drives or watches it. It lives in its own Go
module at the repository root, not in here, since it's an AI player rather
than test/dev infrastructure -- see its own README for the details.

And two tools for building without depending on Docker Hub being reachable
(see [Building](../README.md#building-zcxdoom) in the main README for why):

- **`offlinebuild`** -- what you actually run. Loads this repo's vendored
  base images (`vendor/images/`) and drives `docker buildx build` with them.
- **`vendorimages`** -- a maintainer-only tool that (re)creates the files in
  `vendor/images/` in the first place. You won't need this unless you're
  adding a new architecture or intentionally updating a base image.

## Building

From the repository root, for your own machine:

```console
$ go build -o vncharness ./tools/vncharness
```

Or run it directly without a separate build step:

```console
$ go run ./tools/vncharness screenshot -addr localhost:5901 -password idbehold -out shot.png
```

Cross-compiling for another OS is a one-line `GOOS`/`GOARCH` change, no
extra tooling required:

```console
$ GOOS=windows GOARCH=amd64 go build -o vncharness.exe ./tools/vncharness
$ GOOS=darwin  GOARCH=arm64 go build -o vncharness     ./tools/vncharness   # Apple Silicon Mac
$ GOOS=darwin  GOARCH=amd64 go build -o vncharness     ./tools/vncharness   # Intel Mac
$ GOOS=linux   GOARCH=amd64 go build -o vncharness     ./tools/vncharness
```

## Using vncharness

```console
$ vncharness screenshot -addr localhost:5901 -password idbehold -out shot.png
wrote shot.png (640x480, server name "abc123:99")

$ vncharness key -addr localhost:5901 -password idbehold -keys "escape,down,return"
sent escape
sent down
sent return
```

`-keys` accepts the names in `rfb.Keysyms` (`escape`, `return`/`enter`,
`space`, `tab`, `backspace`, `shift`, `ctrl`, `alt`, `up`, `down`, `left`,
`right`) or any single printable character (`a`, `5`, ...), comma-separated.

### Recording

```console
$ vncharness record -addr localhost:5901 -password idbehold -out game.mp4 -fps 10 -duration 30s
wrote game.mp4 (243 frames, 24.3s, avg 10.0 fps)
```

Omit `-duration` to record until interrupted with Ctrl-C. This shells out
to `ffmpeg` if it's on `PATH` -- the one place in `tools/` that isn't
stdlib-only, since it's common enough not to bundle and specific enough
not to reimplement. Without `ffmpeg`, it automatically falls back to
writing a plain PNG sequence to the `-out` path instead (and prints the
`ffmpeg` command to assemble it yourself later), so the command still
fully works with nothing extra installed.

Because the game's x11vnc server runs with `-shared` (see the main
README), you can record a session while something else -- a human viewer,
or `aiplay` -- is simultaneously connected and driving it; none of these
clients can kick another off.

## Running the smoke test

The test has two modes, chosen by one environment variable, so it works
the same whether zcxdoom is running right here or somewhere else entirely
-- another host, a VM, a different container, a real deployment:

**Point it at something already running** (needs no Docker, no source
checkout on the machine running the test -- just network access to the
address):

```console
$ SMOKE_VNC_ADDR=some-host:5901 SMOKE_VNC_PASSWORD=idbehold \
    go test ./tools/smoketest/... -v
```

**Or let it manage its own container** (needs `docker` on PATH; builds
the image, runs it, tests it, tears it down):

```console
$ go test ./tools/smoketest/... -v
```

Either way it checks two things: that the screen is actually rendering
something (not a blank framebuffer), and that pressing Escape visibly
changes it (proving keyboard input reaches the game, not just that a
socket is open). A third test, `TestWrongPasswordIsRejected`, confirms
authentication actually fails closed.

Both the initial connection and the "does a key press register" check
retry for a few seconds before failing: x11vnc and psdoom take a moment to
settle right after the container starts, and (discovered while building
this) the very first input event in that window can be silently dropped.
A test that failed on that would be flaky, not useful. (The same
characteristic shows up in `aiplay`'s continuous decision loop, but never
needs a retry there -- a dropped key just costs one wasted tick before the
next one tries again automatically.)

## Browser-based viewing (webvnc)

`tools/webvnc` is a small, optional sidecar image -- not part of the core
game image -- that lets you watch a running zcxdoom instance in a plain
web browser instead of a VNC viewer app, using
[noVNC](https://github.com/novnc/noVNC) (a VNC client that runs entirely
as a web page) and `websockify` (the WebSocket-to-TCP bridge noVNC needs,
since browsers can't open raw TCP sockets).

```console
$ docker build -t zcxdoom-webvnc ./tools/webvnc
$ docker run -d --name zcxdoom-webvnc --network container:zcxdoom -p 6080:6080 zcxdoom-webvnc
```

`--network container:zcxdoom` shares the game container's network
namespace, so `webvnc`'s default `VNC_HOST=localhost` reaches it directly
(swap in `-e VNC_HOST=<name> -e VNC_PORT=5900` plus a shared user-defined
network instead, if you'd rather not share the namespace). Then open
`http://localhost:6080/vnc.html` in a browser.

This is a separate image on purpose: unlike the core game image, it needs
network access at build time (to fetch noVNC), so it's kept out of the
offline, vendored build path entirely rather than compromise it. Building
it needs nothing beyond a normal `docker build` with internet access; it
was verified by driving the exact same `pip install` and noVNC-fetch steps
by hand and then confirming a real WebSocket client receives actual RFB
protocol bytes from a live zcxdoom container through the resulting
websockify proxy.

Like recording, this only works because of `-shared`: `webvnc` is just
another VNC client, so it coexists fine with a human viewer, `aiplay`, or
a `vncharness record` session all watching or driving the same instance
at once.

## Building offline: vendored base images

`vendor/images/` holds gzipped `docker save` tarballs of this project's two
Docker Hub base images (`ubuntu:20.04`, `golang:1.17-alpine`), one per
supported architecture (`amd64`, `arm64`, `s390x`) -- about 390MB total,
committed as plain files (no Git LFS: LFS's free bandwidth quota is 1GB a
*month*, which a handful of CI runs pulling ~130MB each would exhaust
almost immediately; plain git objects have no such quota). Files over
GitHub's 100MB single-file limit (the Go toolchain, at ~107MB compressed)
are split into `.tar.gz.part-NNN` chunks and reassembled when loaded.

**`offlinebuild`** is what you run day to day -- see
[Building zcxdoom](../README.md#building-zcxdoom) in the main README. It
loads the right files for `-arch` into Docker under fixed local tags
(`zcxdoom-vendor/ubuntu-20.04:<arch>`, etc.) and passes those tags to the
Dockerfile's `UBUNTU_IMAGE`/`GOLANG_IMAGE` build args, so the `FROM` lines
resolve instantly from the local image store -- BuildKit never attempts a
registry round-trip for them, so there's nothing for Docker Hub's rate
limit to reject.

**`vendorimages`** is the maintainer-only tool that produces those files.
It pins an exact digest per (image, architecture) in its source rather than
resolving a moving tag, so vendoring is reproducible and always matches
what was actually tested. Run it again only when you want to:

```console
$ go run ./tools/vendorimages                    # refresh all 3 architectures
$ go run ./tools/vendorimages -arch arm64         # just one
```

It needs `docker` and real network access to Docker Hub (there's no way
around that for the one machine that does the initial vendoring); update
the `digests` map in `tools/vendorimages/main.go` first if you're bumping
to a new base image version.

**What's deliberately not vendored:** the Dockerfile's `apt-get install`
steps (compiler toolchain, SDL libraries, x11vnc, xvfb) still fetch from
Ubuntu's own package archives over the network. That's a separate, far
less restrictive service than Docker Hub -- it isn't the thing that was
actually rate-limiting builds -- and vendoring its full dependency closure
for 3 architectures would roughly double this directory's size for a
problem that, in practice, wasn't the one being hit.
