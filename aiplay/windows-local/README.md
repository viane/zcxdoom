# Windows-local deploy

Runs the whole `aiplay` stack -- the game, a self-hosted Kev, Ollama, and a
browser-based viewer -- on one machine with one command. Built and tested
for Windows 11 + WSL2 + Docker Desktop with an NVIDIA GPU, but nothing
here is actually Windows-specific: the same `deploy.sh` works on native
Linux + Docker too, GPU or not.

## Prerequisites

- Docker Desktop, with WSL2 integration enabled for whichever distro
  you're running this from (`wsl -l -v` from PowerShell should show it as
  version 2; Docker Desktop's Settings -> Resources -> WSL Integration
  should have that distro toggled on).
- For GPU acceleration (optional, but the whole point of running this on
  your own machine instead of a cloud CPU box): an NVIDIA GPU, a
  reasonably current driver (any driver from the last couple of years
  already includes WSL2 CUDA support), and Docker Desktop's GPU support
  turned on. `deploy.sh` checks this actually works before relying on it,
  not just that a GPU is present.
- Nothing else. `kev` and `aiplay` are both built into images by this
  setup -- no separate Node, pnpm, or Go install needed on the host.

## Running it

```console
$ ./deploy.sh
```

This checks Docker, Compose, RAM, and GPU/VRAM; picks an Ollama model
sized to what it finds (or asks before falling back to CPU); builds all
five images; brings the stack up; pulls the model live (never vendored --
see aiplay/README.md and the main README's stance on vendoring); and
prints where to watch.

Flags:

| Flag | Effect |
|---|---|
| `--cpu` | Skip GPU detection entirely, run Ollama on CPU |
| `--yes`, `-y` | Don't ask before falling back to CPU |
| `--model=NAME` | Use this Ollama model instead of the auto-picked one |

## Watching it play

```
Browser (preferred): http://localhost:6080/vnc.html
Any VNC client:      localhost:5901  (password: idbehold)
```

Both work at the same time, and alongside each other, or a recording
(`go run ../../tools/vncharness record ...` from the repo root) -- the
game's VNC server runs with `-shared` specifically so nothing here can
kick anything else off. The browser viewer needs nothing installed; a
standalone client (TigerVNC, RealVNC Viewer, macOS Screen Sharing, etc.)
works exactly the same way it would against any other zcxdoom deployment.

## What each check is actually for

- **Docker / Compose V2 present and responding** -- the obvious baseline.
  Compose V2 specifically (the `docker compose` plugin, not the legacy
  standalone `docker-compose`) because GPU device reservations
  (`docker-compose.gpu.yml`) generally aren't supported by the legacy one.
- **RAM** -- informational, but WSL2 caps its own memory independently of
  the host's total RAM (see `.wslconfig`), so "the PC has 32GB" doesn't
  always mean WSL2 can see it.
- **GPU present** (`nvidia-smi`) -- just detection.
- **Docker can actually use the GPU** (`docker run --gpus all ...`) --
  the check that matters most. A present-but-unusable GPU is a very
  common state (Docker Desktop's GPU toggle off, a stale driver) and
  fails silently otherwise: Ollama would just quietly run on CPU with no
  clear signal why it's slow.
- **VRAM amount** -- picks the model tier (roughly: 10GB+ gets
  `qwen3.5:9b`, the exact model/backend Kev's own published benchmarks
  were measured on; 5GB+ gets a mid-size model; below that, or no GPU,
  gets something small enough to still be usable on CPU). Override any
  time with `--model=`.

## Troubleshooting

**"docker: 'compose' is not a docker command"** -- you have the legacy
standalone `docker-compose` but not the V2 plugin. Update Docker Desktop;
recent versions bundle Compose V2 by default.

**GPU detected but Docker can't see it** -- in Docker Desktop, check
Settings -> Resources -> GPU (or similar, varies by version) is enabled
for your WSL2 distro. Confirm the driver itself supports WSL2 CUDA by
running `nvidia-smi` directly in WSL2 (outside Docker) first -- if that
fails too, it's a driver/WSL2 issue, not a Docker one.

**Model pull is slow or fails** -- it's a live pull by design (see `#2`
in the top-level task this setup came from: no model caching), so it
depends on your own connection and Ollama's registry being reachable.
Retry with `docker compose exec ollama ollama pull <model>` directly to
see the actual error.

**Everything's running but the AI won't move / Kev errors in logs** -- if
you changed `--model` after the fact, confirm that exact model was
actually pulled (`docker compose exec ollama ollama list`). `aiplay`
falls back to a simple built-in policy whenever Kev is unreachable or
erroring (see aiplay/README.md), so it'll keep playing (badly) rather
than crash -- check `docker compose logs kev` for the real error.

## Rebuilding after a code change

```console
$ docker compose build aiplay   # or kev, zcxdoom, webvnc
$ docker compose up -d aiplay
```

`aiplay` retries its connection to the game for up to 30s
(`-connect-timeout`) on startup, so restarting it alone (without also
restarting `zcxdoom`) is safe.

These plain `docker compose` commands pick up the model `deploy.sh`
chose because it records it in `.env` (gitignored, rewritten on every
run), which Compose reads automatically. Without that file Compose would
fall back to the `llama3.2:1b` default in `docker-compose.yml` and
recreate `kev` pointing at a model nobody pulled -- which doesn't crash
anything, it just makes `aiplay` fall back to its built-in policy and
look inexplicably worse. Pass `--model=NAME` to `deploy.sh` to change it.

## Stopping

```console
$ docker compose down
```

Add `-v` to also drop the Ollama model cache volume (`ollama-data`), so
the next `deploy.sh` run re-pulls the model from scratch.
