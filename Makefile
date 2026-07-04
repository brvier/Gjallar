BINARY  := gjallar
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test vet fmt run clean install

all: build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

run: build
	./$(BINARY) -config gjallar.yaml

clean:
	rm -f $(BINARY)

install: build
	install -Dm755 $(BINARY) /usr/local/bin/$(BINARY)
