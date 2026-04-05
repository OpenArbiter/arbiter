-include .env
export

.PHONY: build test test-integration test-e2e test-all lint fmt vet \
       docker-up docker-down docker-build migrate \
       harness-scenarios harness-live \
       security-scan semgrep trufflehog govulncheck \
       clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Build
build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/arbiter ./cmd/arbiter/

build-harness:
	go build -o bin/harness ./cmd/harness/

# Tests
test:
	go test -race -coverprofile=coverage.out ./...

test-integration:
	go test -race -tags=integration ./...

test-e2e:
	go test -race -tags=e2e ./...

test-all: test test-integration test-e2e

test-full: test test-integration harness-scenarios
	@echo "All tests passed."

# Code quality
lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

# Security
security-scan: semgrep trufflehog govulncheck

semgrep:
	docker run --rm -v "$$(pwd):/src" semgrep/semgrep semgrep scan \
		--config auto --config "p/golang" --config "p/security-audit" \
		--config "p/secrets" --config "p/owasp-top-ten" --error /src

trufflehog:
	docker run --rm -v "$$(pwd):/src" trufflesecurity/trufflehog:latest \
		filesystem --only-verified /src

govulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Docker
docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-build:
	docker compose build

# Database
migrate:
	atlas migrate apply --url "$$ARBITER_DB_URL"

migrate-new:
	atlas migrate diff --to "file://migrations" --dev-url "docker://postgres/16"

# Harness
harness-scenarios:
	go run ./cmd/harness/ scenario all

harness-live:
	go run ./cmd/harness/ live

# Cleanup
clean:
	rm -rf bin/ coverage.out
