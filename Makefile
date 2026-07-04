BINARY  := gjallar
# Version = first release heading in CHANGELOG.md, e.g. "## [0.1.0] - ..."
VERSION ?= $(shell sed -n 's/^## \[\([^]]*\)\].*/\1/p' CHANGELOG.md | head -1)
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
