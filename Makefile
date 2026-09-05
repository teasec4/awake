BINARY  := awake
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILT   := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILT)

.PHONY: build test vet fmt install clean release

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/awake

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

install:
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/awake

release:
	goreleaser release --clean

clean:
	rm -f $(BINARY)
