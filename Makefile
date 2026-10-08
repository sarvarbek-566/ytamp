PREFIX  ?= $(HOME)/.local
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo 0.1.0)
GO      ?= $(shell command -v go 2>/dev/null || echo "mise exec go -- go")

.PHONY: build install uninstall test run clean

build:
	$(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/ytamp ./cmd/ytamp

install: build
	install -Dm755 bin/ytamp $(PREFIX)/bin/ytamp
	@echo "✓ o'rnatildi: $(PREFIX)/bin/ytamp"

uninstall:
	rm -f $(PREFIX)/bin/ytamp

test:
	$(GO) test ./...

run: build
	./bin/ytamp

clean:
	rm -rf bin
