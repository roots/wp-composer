.PHONY: build install dev test integration lint clean tailwind db-restore

VAULT_FILE ?= deploy/ansible/group_vars/production/vault.yml

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

# Integration tests (requires composer)
integration:
	go test -tags=integration -count=1 -timeout=5m -v ./internal/integration/...

# Lint (matches CI: golangci-lint + gofmt + go vet + go mod tidy)
lint:
	$(shell go env GOPATH)/bin/golangci-lint run ./...
	@if [ -n "$$(gofmt -s -l .)" ]; then echo "gofmt needed:"; gofmt -s -l .; exit 1; fi
	go vet ./...
	go mod tidy -diff

# Restore production database from R2 (reads secrets from Ansible vault)
db-restore:
	@eval $$(ansible-vault view --vault-password-file deploy/ansible/.vault_pass $(VAULT_FILE) | yq -r \
		'"export LITESTREAM_BUCKET=\(.vault_r2_litestream_bucket) R2_ENDPOINT=\(.vault_r2_endpoint) R2_ACCESS_KEY_ID=\(.vault_r2_access_key_id) R2_SECRET_ACCESS_KEY=\(.vault_r2_secret_access_key)"') && \
		export DB_PATH=./storage/wpcomposer.db && \
		echo "LITESTREAM_BUCKET=$$LITESTREAM_BUCKET" && \
		echo "R2_ENDPOINT=$$R2_ENDPOINT" && \
		echo "R2_ACCESS_KEY_ID=$$R2_ACCESS_KEY_ID" && \
		echo "R2_SECRET_ACCESS_KEY=$$R2_SECRET_ACCESS_KEY" && \
		go run ./cmd/wpcomposer db restore --force

# Remove build artifacts
clean:
	rm -f wpcomposer
	rm -rf storage/
