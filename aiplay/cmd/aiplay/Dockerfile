# Containerizes the aiplay orchestrator. Build with the REPO ROOT as
# context, not this directory -- aiplay imports zcxdoom/tools/rfb via the
# `replace zcxdoom => ../` in aiplay/go.mod, so the build needs both
# tools/rfb and aiplay/ visible at the same relative layout as the real
# repo:
#
#   docker build -f aiplay/cmd/aiplay/Dockerfile -t aiplay .
#
# (from the repository root). windows-local/docker-compose.yml does this.
#
# Needs no network beyond pulling the golang:1.21-alpine base image itself:
# aiplay has no external module dependencies, only the local replace, so
# `go mod download` is a no-op.
FROM golang:1.21-alpine AS build
WORKDIR /repo
COPY go.mod ./
COPY tools/rfb ./tools/rfb
COPY aiplay ./aiplay
WORKDIR /repo/aiplay
RUN go mod download
RUN CGO_ENABLED=0 go build -o /aiplay ./cmd/aiplay

FROM alpine:3.20
# Copied from the build stage rather than `apk add ca-certificates`: the
# golang:1.21-alpine image already carries a full CA bundle (Go modules
# need HTTPS too), so this needs no extra network fetch or package
# install in the final image at all -- one less thing that can fail on a
# machine with restrictive package-mirror access.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /aiplay /usr/local/bin/aiplay
ENTRYPOINT ["/usr/local/bin/aiplay"]
