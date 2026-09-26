FROM golang:1.17-alpine AS build-kubedoom
WORKDIR /go/src/kubedoom
ADD go.mod .
ADD kubedoom.go .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o kubedoom .

FROM ubuntu:20.04 AS build-doom
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

FROM ubuntu:20.04 AS build-converge
WORKDIR /build
RUN mkdir -p \
  /build/root \
  /build/usr/bin \
  /build/usr/local/games
COPY assets/doom1.wad /build/root/doom1.wad
COPY --from=build-kubedoom /go/src/kubedoom/kubedoom /build/usr/bin
COPY --from=build-doom /usr/local/games/psdoom /build/usr/local/games

FROM ubuntu:20.04
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
ENTRYPOINT ["/usr/bin/kubedoom"]
