.PHONY: build install dev test smoke lint clean tailwind

TAILWIND ?= ./bin/tailwindcss

# Download Tailwind standalone CLI if missing
tailwind-install:
	@if [ ! -f $(TAILWIND) ]; then \
		mkdir -p bin; \
		ARCH=$$(uname -m); \
		OS=$$(uname -s | tr '[:upper:]' '[:lower:]'); \
		case "$$OS" in darwin) OS=macos ;; esac; \
		case "$$ARCH" in \
			x86_64) ARCH=x64 ;; \
			aarch64|arm64) ARCH=arm64 ;; \
		esac; \
		echo "Downloading tailwindcss for $$OS-$$ARCH..."; \
		curl -sLo $(TAILWIND) "https://github.com/tailwindlabs/tailwindcss/releases/latest/download/tailwindcss-$$OS-$$ARCH"; \
		chmod +x $(TAILWIND); \
	fi

# Generate CSS from templates
tailwind: tailwind-install
	$(TAILWIND) -i internal/http/tailwind-input.css -o internal/http/static/assets/styles/app.css --minify

# Build the binary
build: tailwind
	go build -o wpcomposer ./cmd/wpcomposer

# Install to $GOPATH/bin
install:
	go install ./cmd/wpcomposer

# Live-reload dev server (migrations, seed data, serve)
dev: tailwind-install
	air

# Run all tests
test:
	go test ./...

# End-to-end smoke test (requires composer, sqlite3)
smoke: build
	./test/smoke_test.sh

# Lint (matches CI: golangci-lint + gofmt + go vet + go mod tidy)
lint:
	$(shell go env GOPATH)/bin/golangci-lint run ./...
	@if [ -n "$$(gofmt -s -l .)" ]; then echo "gofmt needed:"; gofmt -s -l .; exit 1; fi
	go vet ./...
	@go mod tidy && if [ -n "$$(git diff --name-only -- go.mod go.sum)" ]; then echo "go mod tidy needed"; exit 1; fi

# Remove build artifacts
clean:
	rm -f wpcomposer
	rm -rf storage/
