FROM golang:1.25.6-bookworm AS dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
FROM dev AS test
RUN go test -race ./... && go vet ./...
RUN go build -o bin/scoretex ./cmd/scoretex && python3 scripts/validate.py
FROM dev AS build
ARG VERSION=devel
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /scoretex ./cmd/scoretex
FROM scratch AS runtime
COPY --from=build /scoretex /scoretex
USER 65532:65532
ENTRYPOINT ["/scoretex"]
