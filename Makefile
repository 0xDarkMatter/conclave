VERSION := 1.3.0-dev
BINARY := conclave
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build install release clean test lint check vet fmt-check models-check

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

# THE gate. Everything that must be true before a commit lands runs here, in
# one command, in the order that fails cheapest first. CI runs exactly this.
check: vet fmt-check test models-check
	@echo "check: all gates passed"

vet:
	@echo "==> go vet"
	@go vet ./...

# gofmt -l lists files that WOULD change; the gate is that it prints nothing.
# gofmt itself exits 0 either way, so the emptiness is the assertion.
fmt-check:
	@echo "==> gofmt"
	@out=$$(gofmt -l . 2>/dev/null); 	if [ -n "$$out" ]; then 		echo "gofmt: these files need formatting:"; 		echo "$$out"; 		exit 1; 	fi

# Catalog drift check. Needs the network, so it degrades to a skip rather than
# a failure when the catalog cannot be reached or is switched off. A real drift
# (a compiled default OpenRouter no longer lists) still fails the gate.
models-check: build
	@echo "==> conclave models --check"
	@if [ -n "$$CONCLAVE_NO_PRICING" ]; then 		echo "skipped: CONCLAVE_NO_PRICING is set"; 	else 		out=$$(./bin/$(BINARY) models --check 2>&1); rc=$$?; 		echo "$$out"; 		if [ $$rc -ne 0 ]; then 			case "$$out" in 				*"catalog unavailable"*|*"no catalog available"*|*"pricing catalog is disabled"*) 					echo "skipped: pricing catalog unreachable (offline?)";; 				*) exit $$rc;; 			esac; 		fi; 	fi

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
	@echo "  check    - THE gate: vet + gofmt + tests + models --check"
	@echo "  build    - Build for current platform"
	@echo "  install  - Build and install to /usr/local/bin"
	@echo "  release  - Build for all platforms"
	@echo "  test     - Run tests"
	@echo "  lint     - Run linter"
	@echo "  clean    - Remove build artifacts"
	@echo "  tidy     - Tidy go.mod"
