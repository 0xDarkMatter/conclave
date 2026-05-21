VERSION := 1.1.0
BINARY := conclave
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build install release clean test lint

# Build for current platform
build:
	go build $(LDFLAGS) -o bin/$(BINARY) .

# Install to ~/.local/bin (or use sudo make install-global for /usr/local/bin)
install: build
	@mkdir -p $(HOME)/.local/bin
	cp bin/$(BINARY) $(HOME)/.local/bin/
	@echo "Installed to $(HOME)/.local/bin/$(BINARY)"
	@echo "Ensure $(HOME)/.local/bin is in your PATH"

# Install to /usr/local/bin (requires sudo)
install-global: build
	cp bin/$(BINARY) /usr/local/bin/

# Build for all platforms
release:
	@mkdir -p bin
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		go build $(LDFLAGS) \
		-o bin/$(BINARY)-$${platform%/*}-$${platform#*/}$$(if [ "$${platform%/*}" = "windows" ]; then echo ".exe"; fi) .; \
		echo "Built bin/$(BINARY)-$${platform%/*}-$${platform#*/}"; \
	done

# Run tests
test:
	go test -v ./...

# Run linter
lint:
	golangci-lint run

# Clean build artifacts
clean:
	rm -rf bin/

# Tidy dependencies
tidy:
	go mod tidy

# Show help
help:
	@echo "Conclave CLI Makefile"
	@echo ""
	@echo "Targets:"
	@echo "  build    - Build for current platform"
	@echo "  install  - Build and install to /usr/local/bin"
	@echo "  release  - Build for all platforms"
	@echo "  test     - Run tests"
	@echo "  lint     - Run linter"
	@echo "  clean    - Remove build artifacts"
	@echo "  tidy     - Tidy go.mod"
