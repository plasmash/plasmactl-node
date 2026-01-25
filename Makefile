.PHONY: build test lint clean deps verify generate fmt check-fmt container-build

GO := go
GOLINT := golangci-lint
GO_IMAGE := golang:1.25

MODULE := github.com/plasmash/plasmactl-env

build:
	$(GO) build ./...

# Build using container (for Go 1.25)
container-build:
	docker run --rm -v $(PWD):/app -w /app $(GO_IMAGE) go build ./...

# Run go mod tidy in container
container-tidy:
	docker run --rm -v $(PWD):/app -w /app $(GO_IMAGE) go mod tidy

test:
	$(GO) test -v ./...

lint:
	$(GOLINT) run ./...

clean:
	$(GO) clean
	rm -rf .plasma/

# Download dependencies
deps:
	$(GO) mod download
	$(GO) mod tidy

# Verify module
verify:
	$(GO) mod verify

# Run go generate
generate:
	$(GO) generate ./...

# Format code
fmt:
	$(GO) fmt ./...

# Check formatting
check-fmt:
	@test -z "$$(gofmt -l .)" || (echo "Files not formatted:"; gofmt -l .; exit 1)
