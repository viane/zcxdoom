# Optional sidecar: browser-based live viewing of a running zcxdoom
# instance, via noVNC (a VNC client that runs entirely as a web page) and
# websockify (the WebSocket<->TCP proxy noVNC needs, since browsers can't
# open raw TCP sockets).
#
# Deliberately not part of the core game image: it needs network access to
# fetch noVNC at build time, unlike the core image, which can build fully
# offline from vendor/images/ (see ../../README.md#building-zcxdoom). This
# is purely an optional viewer that talks to an already-running zcxdoom
# instance over the network -- it never touches the game image or its
# build.
FROM python:3.12-alpine

# NOVNC_VERSION pins a release tag rather than tracking a moving branch, the
# same reproducibility reasoning as vendorimages' pinned base-image digests
# (see tools/README.md). Check https://github.com/novnc/noVNC/tags for newer
# releases when bumping it.
ARG NOVNC_VERSION=1.7.0

# Fetches and extracts noVNC with nothing beyond what's already in the base
# image (Python's own urllib+tarfile), rather than adding git and Alpine's
# package index as extra build-time dependencies just to clone one repo.
# The extracted top-level directory name isn't hardcoded, since GitHub's
# archive naming for a given tag isn't guaranteed to be exactly
# "noVNC-<version>" across all cases.
RUN pip install --no-cache-dir websockify \
    && python3 -c "\
import urllib.request, tarfile, io, os; \
url = 'https://github.com/novnc/noVNC/archive/refs/tags/v${NOVNC_VERSION}.tar.gz'; \
data = urllib.request.urlopen(url).read(); \
tarfile.open(fileobj=io.BytesIO(data)).extractall('/tmp/novnc-extract'); \
top = os.listdir('/tmp/novnc-extract')[0]; \
os.rename(f'/tmp/novnc-extract/{top}', '/novnc')"

# The VNC server this sidecar proxies to. Point it at a running zcxdoom
# instance: the game's own container name on a shared Docker network, or
# "localhost" if run with --network container:<game-container>.
ENV VNC_HOST=localhost
ENV VNC_PORT=5900

EXPOSE 6080

CMD websockify --web /novnc 6080 ${VNC_HOST}:${VNC_PORT}
