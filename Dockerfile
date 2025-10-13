####################### builder image

FROM golang:1.25.1 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY server/ ./server/

RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux go install ./server/...

####################### runtime image

FROM debian:trixie-slim AS runtime

ENV DEBIAN_FRONTEND=noninteractive
ENV DATA_DIR=/data
RUN mkdir -p $DATA_DIR

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates tini && rm -rf /var/lib/apt/lists/*

######################## mt image

FROM runtime AS mt

ENV SAVE_MODE=periodic

COPY --from=builder /go/bin/mt /usr/local/bin/mt

ENTRYPOINT ["/usr/bin/tini", "--", "mt"]
