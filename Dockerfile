# UBUNTU_IMAGE/GOLANG_IMAGE default to their normal Docker Hub names, so a
# plain `docker build .` with no other setup works exactly as before. To
# build without any Docker Hub access, run `go run ./tools/offlinebuild`
# instead: it loads this project's vendored copies of these two images
# (vendor/images/) and passes their local tags here. See tools/README.md.
ARG UBUNTU_IMAGE=ubuntu:20.04
ARG GOLANG_IMAGE=golang:1.17-alpine

FROM ${GOLANG_IMAGE} AS build-zcxdoom
WORKDIR /go/src/zcxdoom
ADD go.mod .
ADD main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o zcxdoom .

FROM ${UBUNTU_IMAGE} AS build-doom
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y \
  -o APT::Install-Suggests=0 \
  --no-install-recommends \
  build-essential \
  libsdl-mixer1.2-dev \
  libsdl-net1.2-dev \
  gcc
ADD /dockerdoom /dockerdoom
WORKDIR /dockerdoom/trunk
RUN ./configure && make && make install

FROM ${UBUNTU_IMAGE} AS build-converge
WORKDIR /build
RUN mkdir -p \
  /build/root \
  /build/usr/bin \
  /build/usr/local/games
COPY assets/doom1.wad /build/root/doom1.wad
COPY --from=build-zcxdoom /go/src/zcxdoom/zcxdoom /build/usr/bin
COPY --from=build-doom /usr/local/games/psdoom /build/usr/local/games

FROM ${UBUNTU_IMAGE}
ARG VNCPASSWORD=idbehold
RUN apt-get update && apt-get install -y \
  -o APT::Install-Suggests=0 \
  --no-install-recommends \
  libsdl-mixer1.2 \
  libsdl-net1.2 \
  x11vnc \
  xvfb \
  && rm -rf /var/lib/apt/lists/*
RUN mkdir /root/.vnc && x11vnc -storepasswd "${VNCPASSWORD}" /root/.vnc/passwd
COPY --from=build-converge /build /
WORKDIR /root
ENTRYPOINT ["/usr/bin/zcxdoom"]
