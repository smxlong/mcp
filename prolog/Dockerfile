# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# dev: SWI-Prolog runtime plus the Go toolchain, for `go test`. The tests drive
# a real swipl binary rather than a mock, so they need both.
# ---------------------------------------------------------------------------
FROM swipl:10.1.13 AS dev
COPY --from=golang:1.25-bookworm /usr/local/go /usr/local/go
# gcc is needed by the race detector, which the concurrency tests rely on.
RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc libc6-dev \
    && rm -rf /var/lib/apt/lists/*
ENV PATH=/usr/local/go/bin:/go/bin:$PATH \
    GOPATH=/go \
    GOFLAGS=-buildvcs=false
WORKDIR /src

# ---------------------------------------------------------------------------
# build: compile a static binary for the target platform. Cross-compiling from
# the build platform keeps multi-arch builds out of emulation.
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=devel
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/mcprolog ./cmd/mcprolog

# ---------------------------------------------------------------------------
# runtime: SWI-Prolog plus the server binary. driver.pl is embedded in the
# binary with go:embed, so nothing else needs to be copied.
# ---------------------------------------------------------------------------
FROM swipl:10.1.13 AS runtime
ARG VERSION=devel
LABEL org.opencontainers.image.title="mcprolog" \
      org.opencontainers.image.description="MCP server exposing SWI-Prolog as logic tools" \
      org.opencontainers.image.source="https://github.com/smxlong/mcp" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"
# The state directory must exist in the image and be owned by the runtime user.
# Docker creates a mount point that is missing from the image as root-owned, so
# a named volume mounted at a path that does not exist here would be unwritable.
RUN useradd --create-home --uid 10001 prolog \
    && mkdir -p /home/prolog/state \
    && chown -R prolog:prolog /home/prolog
COPY --from=build /out/mcprolog /usr/local/bin/mcprolog
USER prolog
WORKDIR /home/prolog
VOLUME ["/home/prolog/state"]
ENV MCPROLOG_STATE=/home/prolog/state/kb.json
ENTRYPOINT ["/usr/local/bin/mcprolog"]
