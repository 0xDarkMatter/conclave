VERSION := 1.3.0
BINARY := conclave
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build install release clean test lint check vet fmt-check race models-check

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
# one command, in the order that fails cheapest first. CI runs exactly this
# command, so the two cannot drift.
check: vet fmt-check test race models-check
	@echo "check: all gates passed"

vet:
	@echo "==> go vet"
	@go vet ./...

# gofmt -l lists files that WOULD change; the gate is that it prints nothing.
# Files are enumerated via `go list`, never `gofmt -l .`: gofmt recurses into
# dot-directories, and the repo root package's dir IS the repo root, so a bare
# `.` (or the root package dir) sweeps every nested .claude/worktrees/* checkout
# and fails the gate on other sessions' files (bit us 2026-09-08).
# gofmt itself exits 0 either way, so the emptiness is the assertion.
fmt-check:
	@echo "==> gofmt"
	@out=$$(gofmt -l $$(go list -f '{{range .GoFiles}}{{$$.Dir}}/{{.}} {{end}}{{range .TestGoFiles}}{{$$.Dir}}/{{.}} {{end}}{{range .XTestGoFiles}}{{$$.Dir}}/{{.}} {{end}}' ./...) 2>/dev/null); 	if [ -n "$$out" ]; then 		echo "gofmt: these files need formatting:"; 		echo "$$out"; 		exit 1; 	fi

# The race detector needs a working cgo toolchain. Windows commonly has none,
# and the concurrent code here is not platform-specific, so skipping there
# costs nothing while a missing toolchain would otherwise fail the whole gate.
race:
	@echo "==> go test -race"
	@if [ "$$(go env GOOS)" = "windows" ]; then 		echo "skipped: the race detector needs a cgo toolchain, which Windows often lacks"; 	else 		go test -race ./...; 	fi

# Catalog drift check. `conclave models --check` exits 2 for real drift and 3
# when the catalog cannot be reached, so this branches on the CODE rather than
# grepping the message: a reworded error used to be able to turn a hard failure
# into a silent skip.
models-check: build
	@echo "==> conclave models --check"
	@./bin/$(BINARY) models --check; rc=$$?; 	if [ $$rc -eq 3 ]; then 		echo "skipped: pricing catalog unavailable (offline, or CONCLAVE_NO_PRICING set)"; 	elif [ $$rc -ne 0 ]; then 		exit $$rc; 	fi

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
