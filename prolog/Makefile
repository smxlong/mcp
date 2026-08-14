# mcprolog build targets. The tests drive a real swipl, which must be a full
# SWI-Prolog installation: minimal packages such as Debian's swi-prolog-core
# omit library(time) and library(http/json). `make test-docker` runs them in an
# image that has one.

VERSION ?= $(shell git describe --tags --match 'prolog/v*' --dirty 2>/dev/null | sed 's|^prolog/||')
ifeq ($(strip $(VERSION)),)
VERSION := devel
endif

IMAGE   ?= mcprolog
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test test-docker lint fmt image clean

all: build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/mcprolog ./cmd/mcprolog

test:
	go test -race ./...

test-docker:
	docker build --target dev -t $(IMAGE)-dev .
	docker run --rm -v "$(CURDIR):/src" -v mcprolog-gomod:/go/pkg/mod -v mcprolog-gocache:/root/.cache/go-build -w /src $(IMAGE)-dev go test -race ./...

lint:
	golangci-lint run

fmt:
	gofmt -l -w .

image:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .

clean:
	rm -rf bin
