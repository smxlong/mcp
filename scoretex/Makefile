VERSION ?= devel
IMAGE ?= scoretex
.PHONY: build test test-docker lint fmt image validate
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o bin/scoretex ./cmd/scoretex
test:
	CGO_ENABLED=1 go test -race ./...
test-docker:
	docker build --target test -t $(IMAGE)-test .
lint:
	go vet ./...
	golangci-lint run
fmt:
	gofmt -w .
image:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(VERSION) .
validate: build
	python3 scripts/validate.py
