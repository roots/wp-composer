.PHONY: build install dev test smoke lint clean

# Build the binary
build:
	go build -o wpcomposer ./cmd/wpcomposer

# Install to $GOPATH/bin
install:
	go install ./cmd/wpcomposer

# Build and start dev server (migrations, seed data, serve)
dev: build
	ADMIN_ALLOW_CIDR= ./wpcomposer dev --addr :8080

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
