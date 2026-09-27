# aiplay

A computer-use proof of concept: an AI plays zcxdoom over the same VNC
protocol a human would use, with no access to the game's internals.

## Architecture: two systems, two speeds

Following the "System 1 / System 2" split popularized by TypeSafe's Jev and
its self-hostable, API-compatible cousin
[Kev](https://github.com/jaredpalmer/kev) (borrowed, as they did, from
Kahneman's *Thinking, Fast and Slow*):

```
                     ,--------------------------------------------.
                     |  zcxdoom container (x11vnc -shared)         |
                     `--------------------------------------------'
                          |  fast VNC conn        |  slow VNC conn
                          v                        v
   ,-----------------------------------.  ,---------------------------.
   |  System 1 loop (every ~300ms)     |  |  System 2 loop (every ~30s)|
   |  screenshot -> perception.Extract |  |  screenshot -> system2     |
   |  -> doompolicy.Decide -> key press|  |  .Tactic() -> new tactic   |
   `-----------------------------------'  `---------------------------'
                          ^                        |
                          `--- current tactic ------'
                            (shared, mutex-guarded)
```

System 1 is the fast reflex loop: it looks at the screen many times a
second and picks the next key to press. System 2 is slow and deliberate: it
looks at an actual screenshot roughly every 30 seconds and sets a
high-level tactic ("retreat and find ammo") that System 1 reads as context
for its next batch of decisions, but never blocks on.

**Why two separate VNC connections, not one:** `cmd/aiplay/main.go` opens
an independent `rfb.Conn` for each loop rather than sharing one behind a
mutex. This only works because the game's x11vnc server runs with
`-shared` (see the main README); it means System 2's occasional slow
screenshot can never block or interleave with System 1's fast decisions,
and either loop can be understood without thinking about the other's
timing at all.

Running two (or more) simultaneous VNC clients against the same instance
is also what surfaced a real bug in `tools/rfb`: once more than one client
is connected, x11vnc can send an unsolicited Bell or ServerCutText message
between clients, which the RFB client wasn't skipping -- it desynced the
whole connection instead. Found by actually running `aiplay` and a
`vncharness record` session against the same container at once; fixed in
`tools/rfb/rfb.go` (see its `Screenshot` doc comment) and covered by
regression tests in `tools/rfb/rfb_test.go`.

## Why it's a separate Go module

`aiplay` has its own `go.mod`, tied to the root `zcxdoom` module only
through a `replace` directive (see the repo's `go.work`) so it can import
`zcxdoom/tools/rfb`. The game module has zero references back to `aiplay`
-- it doesn't know this exists, same as it doesn't know `tools/` exists.
This keeps the game's own dependency list minimal (it's stdlib-only on
purpose) regardless of what `aiplay` eventually needs, and means this
whole directory could be lifted into its own repository later with no
rework beyond the replace directive.

## The packages

- **`perception`** -- turns a screenshot into the small set of typed facts
  System 1 reasons over (frame-to-frame change, brightness), rather than
  handing over pixels: even TypeSafe's own official Doom demo feeds Jev
  structured state, not images. What's here is a deliberately modest first
  slice, verified against real captured gameplay frames (see its package
  doc and tests). Richer facts -- exact health/ammo via HUD digit reading,
  enemy-visible -- are a natural next step that needs calibration against
  the game's actual rendered layout (Doom draws its HUD inside a scaled,
  bordered 640x480 canvas, not at fixed offsets); it hasn't been done yet.
- **`system1`** -- a generic HTTP client for Kev/Jev's typed
  choice/score/noul question contract (`state` + `questions` map in,
  `answers` map with calibrated probabilities out). Points at either
  TypeSafe's hosted Jev or a self-hosted, API-compatible server (Kev, or
  others) via `-system1-url`; nothing else changes. The wire format was
  verified directly against a running Kev server (its `/openapi.json` and
  a live `/v1/systemone` call) rather than just documentation -- an
  earlier version of this client, written from Kev/Jev's public
  description alone, had the request and response shape wrong in several
  places (arrays where the real API uses maps keyed by id, `kind`/`prompt`/
  `choices`/`bool` instead of the real `type`/`instructions`/`criteria`/
  `noul`) until this was actually run against a real server.
- **`system2`** -- a client for Gemini's `generateContent` API: sends a
  screenshot plus recent history, gets back one sentence of tactical
  instruction. See **Running it** below for the free-tier notes.
- **`doompolicy`** -- the Doom-specific glue: which questions to ask
  System 1, and which key each answer maps to (arrow keys to move/turn,
  Ctrl to fire, Space to use -- psdoom's defaults). Falls back to a simple,
  self-contained heuristic (mostly move forward, turn if stuck, fire
  occasionally) whenever System 1 is unset, unreachable, or errors, so the
  loop never stalls waiting on it.
- **`cmd/aiplay`** -- the orchestrator described above.

## Running it

```console
$ cd aiplay
$ go run ./cmd/aiplay -addr localhost:5901 -password idbehold \
    -system1-url http://localhost:8000 \
    -system2-apikey "$GEMINI_API_KEY"
```

Flags (all optional; omitting both `-system1-url` and an API key still
runs the loop on the built-in fallback policy alone, which is useful for
checking the plumbing works before pointing it at any real backend):

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `localhost:5901` | VNC server address |
| `-password` | `idbehold` | VNC password |
| `-system1-url` | *(empty)* | Base URL of a Kev/Jev-compatible server; empty uses the fallback policy only |
| `-system1-interval` | `300ms` | How often System 1 decides |
| `-system2-apikey` | `$GEMINI_API_KEY` | Gemini API key; empty disables System 2 entirely |
| `-system2-model` | `gemini-2.5-flash` | Gemini model name |
| `-system2-interval` | `30s` | How often System 2 re-evaluates tactics |

### Self-hosting System 1 with Kev

Kev (Apache-2.0) serves the same contract TypeSafe's own SDK expects, so
any self-hosted, API-compatible build works here unchanged -- point
`-system1-url` at wherever it's listening. Two real implementations, both
verified against this client:
[jaredpalmer/kev](https://github.com/jaredpalmer/kev) (Python, PyTorch
models on CUDA/ROCm/MLX) and
[arjun988/kev](https://github.com/arjun988/kev) ("NotJev : Kev", Node.js,
`KEV_BACKEND=mock|ollama|openai`). The latter's `mock` backend needs no
model or GPU at all -- `pnpm install && pnpm build && pnpm --filter
@kev-ai/server start` gets a real, running `/v1/systemone` server in
seconds, useful for exactly what it sounds like: proving the plumbing
works before waiting on a model.

For actual decisions instead of the mock's placeholder ones, its `ollama`
backend (`KEV_BACKEND=ollama`, `KEV_OLLAMA_MODEL=...`) is the one to reach
for without a GPU: Ollama falls back to CPU automatically with no
configuration, where CUDA/ROCm-oriented builds may not. This hasn't been
verified end-to-end in this repo's own development sandbox specifically
because that sandbox's network policy blocks `ollama.com` outright (an
environment-specific restriction, not anything about the code) -- it
should work on any normal machine with internet access.

### System 2 and Gemini's free tier

The default 30-second interval calls Gemini about twice a minute, well
under the free tier's ~10 requests/minute limit (numbers vary slightly by
source; check https://ai.google.dev/gemini-api/docs/pricing for the
current figures). The free tier's daily request cap matters more for long
unattended sessions than short ones. One thing worth knowing before using
it: Google's terms allow using free-tier traffic to improve their
products. Gameplay screenshots aren't sensitive, but it's the real
trade-off of "free," not a hidden one.

### Expect it to be slow on CPU -- that's fine for now

Whatever System 1 backend you point this at, if it's running on CPU
(no GPU), decisions will be visibly slower and gameplay will look choppy
compared to the ~50-150ms latencies GPU-backed System-1 models quote. That's
expected, not a bug: the point of this first version is a correct,
end-to-end loop, not a fast one. Swapping in a GPU-backed System 1 (and a
richer `perception` package alongside it) is the natural next step once
the plumbing here is trusted.

## Testing

```console
$ go test ./...
```

`perception`, `system1`, `system2`, and `doompolicy` all have unit tests
(the two HTTP clients use `httptest` servers standing in for a real Kev or
Gemini endpoint, since neither is available in CI). `cmd/aiplay` itself is
the orchestrator and is exercised by hand against a real container instead
of by an automated test, the same way `tools/smoketest` covers the game
itself -- see the main `tools/README.md`.
