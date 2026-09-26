# tools

Testing and control infrastructure for zcxdoom, built on one small,
dependency-free package:

- **`rfb`** -- a minimal RFB (VNC) protocol client. Just enough to connect,
  authenticate, take a screenshot, and send key presses. Standard library
  only, so anything built on it is a single static binary with no runtime
  dependencies -- copy it to a Windows, macOS, or Linux machine and it
  just runs.
- **`vncharness`** -- a CLI on top of `rfb`, for driving a running
  instance by hand or from a script.
- **`smoketest`** -- a `go test` that uses `rfb` directly to check that a
  zcxdoom instance actually boots and responds to input.

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
A test that failed on that would be flaky, not useful.
