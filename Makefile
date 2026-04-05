.PHONY: build test test-integration test-e2e test-all lint fmt vet docker-up docker-down docker-build migrate clean

# Build
build:
	go build -o bin/arbiter ./cmd/arbiter/

# Tests
test:
	go test -race -coverprofile=coverage.out ./...

test-integration:
	go test -race -tags=integration ./...

test-e2e:
	go test -race -tags=e2e ./...

test-all: test test-integration test-e2e

# Code quality
lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

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

# Cleanup
clean:
	rm -rf bin/ coverage.out
