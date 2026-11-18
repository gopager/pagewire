MODULE   := github.com/gopager/pagewire
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
LDFLAGS  := -X $(MODULE)/internal/buildinfo.Version=$(VERSION)
PLATFORMS ?= linux/amd64,linux/arm64,linux/arm/v7
IMAGE    ?= pagewire

.PHONY: build test lint vet docker docker-local binaries bench man clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/pagewire ./cmd/pagewire

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

bench:
	go test -run xxx -bench . -benchmem ./internal/dsp/...

## man: render the manual page to dist/pagewire.1
man: build
	mkdir -p dist && bin/pagewire manpage > dist/pagewire.1

lint:
	golangci-lint run ./...

## docker: multi-arch static images (needs docker buildx)
docker:
	docker buildx build --platform $(PLATFORMS) --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -f deploy/docker/Dockerfile -t $(IMAGE):$(VERSION) .

## binaries: static Linux binaries for all platforms into dist/
binaries:
	docker buildx build --platform $(PLATFORMS) --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -f deploy/docker/Dockerfile --target artifacts --output type=local,dest=dist .

## docker-local: single-platform image loaded into the local daemon
docker-local:
	docker buildx build --load --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -f deploy/docker/Dockerfile -t $(IMAGE):dev .

clean:
	rm -rf bin dist
