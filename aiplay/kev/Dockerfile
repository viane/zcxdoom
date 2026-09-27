# Containerizes github.com/arjun988/kev ("NotJev : Kev"), an Apache-2.0,
# self-hostable System-1 decision server, API-compatible with the contract
# aiplay/system1 speaks. This keeps a Windows/WSL2 (or any Docker) deploy
# down to "just Docker" -- no separate Node/pnpm install on the host.
#
# Run ./fetch.sh first: it clones the pinned commit into ./src, which this
# build context needs. Fetching happens outside the Dockerfile (see
# fetch.sh's own comment) so this image needs nothing beyond node:20-slim
# itself -- no apt-get, no installing git into the image just to throw it
# away after building.
#
# Like tools/webvnc, the source fetch needs network access and is kept
# out of the core game image's offline, vendored build path on purpose --
# see ../../README.md.
FROM node:20-slim AS build
WORKDIR /kev
COPY src/ .

# packageManager in package.json pins the exact pnpm version; corepack
# reads it, so this always matches what the repo was actually tested with.
RUN corepack enable
RUN pnpm install --frozen-lockfile
RUN pnpm build

FROM node:20-slim
COPY --from=build /kev /kev
WORKDIR /kev/apps/server

ENV KEV_HOST=0.0.0.0
ENV KEV_PORT=3000
EXPOSE 3000

# Running dist/cli.js directly (what "pnpm --filter @kev-ai/server start"
# resolves to) rather than going through pnpm/corepack at runtime avoids
# corepack needing to fetch the pinned pnpm version over the network on
# every fresh container start, for no benefit -- the workspace's
# node_modules (pnpm's symlinked layout) is already in place from the
# build stage either way.
CMD ["node", "dist/cli.js"]
