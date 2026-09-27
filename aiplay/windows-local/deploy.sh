#!/usr/bin/env bash
# Checks the host is actually ready, then builds and starts the whole
# aiplay stack (game + Ollama + Kev + aiplay + a browser viewer) via
# Docker Compose. Written for Windows 11 + WSL2 + Docker Desktop, but
# works the same on any Linux host with Docker -- WSL2 isn't required,
# just detected and mentioned.
#
# See README.md in this directory for what each check verifies, why, and
# how to point this at a different model.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

usage() {
  cat <<'EOF'
usage: ./deploy.sh [--cpu] [--yes] [--model=NAME]

  --cpu          Skip GPU detection entirely and run Ollama on CPU, even
                 if a GPU is present. Slower, but needs no NVIDIA/Docker
                 GPU setup at all.
  --yes, -y      Don't ask for confirmation before falling back to CPU.
  --model=NAME   Use this Ollama model instead of the auto-picked one
                 (e.g. --model=qwen3.5:9b). Same as setting $KEV_MODEL.
EOF
}

skip_gpu_check=false
assume_yes=false
model_override=""
for arg in "$@"; do
  case "$arg" in
    --cpu) skip_gpu_check=true ;;
    --yes|-y) assume_yes=true ;;
    --model=*) model_override="${arg#--model=}" ;;
    --help|-h) usage; exit 0 ;;
    *) echo "unknown flag: $arg" >&2; usage; exit 2 ;;
  esac
done

echo "== Checking prerequisites =="

for tool in docker curl; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "ERROR: '$tool' not found on PATH." >&2
    exit 1
  fi
done

if ! docker info >/dev/null 2>&1; then
  echo "ERROR: docker is installed but not responding. Is Docker Desktop running," >&2
  echo "and is its WSL2 integration enabled for this distro?" >&2
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo "ERROR: 'docker compose' (the Compose V2 plugin, bundled with modern Docker" >&2
  echo "Desktop) was not found. The legacy standalone docker-compose binary isn't" >&2
  echo "supported here -- it generally can't do GPU device reservations." >&2
  exit 1
fi

if grep -qi microsoft /proc/version 2>/dev/null; then
  echo "Running under WSL2."
fi

total_ram_mb=$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo 2>/dev/null || echo 0)
echo "RAM visible to this environment: ${total_ram_mb} MB"
if [ "$total_ram_mb" -gt 0 ] && [ "$total_ram_mb" -lt 8192 ]; then
  echo "WARNING: under 8 GB visible. If your machine has more, WSL2 caps its own" >&2
  echo "memory independently of the host -- see .wslconfig (%UserProfile%\\.wslconfig)." >&2
fi

echo
echo "== Checking for GPU acceleration =="

gpu_ok=false
vram_mb=0
if [ "$skip_gpu_check" = true ]; then
  echo "Skipping GPU detection (--cpu)."
elif ! command -v nvidia-smi >/dev/null 2>&1; then
  echo "No nvidia-smi found -- no NVIDIA GPU visible to this environment."
else
  vram_mb=$(nvidia-smi --query-gpu=memory.total --format=csv,noheader,nounits 2>/dev/null | head -1 | tr -d ' ')
  echo "GPU detected: ${vram_mb} MB VRAM."
  echo "Checking Docker can actually see it (this is the step that most often"
  echo "silently fails even when the GPU itself is fine)..."
  if docker run --rm --gpus all alpine:3.20 sh -c 'command -v nvidia-smi && nvidia-smi -L' >/dev/null 2>&1; then
    gpu_ok=true
    echo "Docker GPU passthrough works."
  else
    echo "WARNING: a GPU was detected, but 'docker run --gpus all' cannot see it." >&2
    echo "This usually means Docker Desktop's GPU support isn't turned on for this" >&2
    echo "WSL2 distro, or the NVIDIA driver needs updating. See this directory's" >&2
    echo "README, Troubleshooting section." >&2
  fi
fi

if [ "$gpu_ok" = false ]; then
  echo
  echo "Proceeding WITHOUT GPU acceleration: Ollama will run on CPU. This works,"
  echo "but decisions will be visibly slower and gameplay choppier -- see"
  echo "aiplay/README.md's \"Expect it to be slow on CPU\" note. That's expected,"
  echo "not a bug."
  if [ "$assume_yes" = false ] && [ "$skip_gpu_check" = false ]; then
    read -r -p "Continue on CPU? [y/N] " reply
    case "$reply" in
      [yY]*) ;;
      *) echo "Aborted."; exit 1 ;;
    esac
  fi
fi

echo
echo "== Picking a model =="

if [ -n "$model_override" ]; then
  kev_model="$model_override"
elif [ -n "${KEV_MODEL:-}" ]; then
  kev_model="$KEV_MODEL"
elif [ "$gpu_ok" = true ]; then
  if [ "$vram_mb" -ge 10000 ]; then
    kev_model="qwen3.5:9b"   # what Kev's own published benchmarks were measured on
  elif [ "$vram_mb" -ge 5000 ]; then
    kev_model="qwen2.5:3b"
  else
    kev_model="llama3.2:1b"
  fi
else
  kev_model="llama3.2:1b"    # small enough to be tolerable on CPU too
fi
echo "Using Ollama model: $kev_model"
echo "(override any time with --model=NAME, or \$KEV_MODEL before rerunning)"

export KEV_MODEL="$kev_model"
export KEV_BACKEND=ollama

compose_files=(-f docker-compose.yml)
if [ "$gpu_ok" = true ]; then
  compose_files+=(-f docker-compose.gpu.yml)
fi

echo
echo "== Fetching Kev's source =="
../kev/fetch.sh

echo
echo "== Building images (first run takes a few minutes) =="
docker compose "${compose_files[@]}" build

echo
echo "== Starting the game and Ollama =="
docker compose "${compose_files[@]}" up -d zcxdoom ollama

echo "Waiting for Ollama..."
ollama_ready=false
for _ in $(seq 1 30); do
  if curl -sf http://localhost:11434/api/tags >/dev/null 2>&1; then
    ollama_ready=true
    break
  fi
  sleep 1
done
if [ "$ollama_ready" = false ]; then
  echo "ERROR: Ollama didn't come up within 30s. Check: docker compose logs ollama" >&2
  exit 1
fi

echo
echo "== Pulling $kev_model (live, not vendored -- this can take a while the"
echo "   first time) =="
docker compose "${compose_files[@]}" exec ollama ollama pull "$kev_model"

echo
echo "== Starting Kev, aiplay, and the web viewer =="
docker compose "${compose_files[@]}" up -d kev aiplay webvnc

echo "Waiting for Kev..."
kev_ready=false
for _ in $(seq 1 30); do
  if curl -sf http://localhost:3000/health >/dev/null 2>&1; then
    kev_ready=true
    break
  fi
  sleep 1
done
if [ "$kev_ready" = false ]; then
  echo "ERROR: Kev didn't come up within 30s. Check: docker compose logs kev" >&2
  exit 1
fi

echo
echo "================================================================"
echo " Ready. Watch the AI play:"
echo "   Browser (preferred): http://localhost:6080/vnc.html"
echo "   Any VNC client:      localhost:5901  (password: idbehold)"
echo "================================================================"
echo
echo "Logs:  docker compose ${compose_files[*]} logs -f aiplay"
echo "Stop:  docker compose ${compose_files[*]} down"
