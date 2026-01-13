#!/bin/sh

set -e

echo "Running Go module tidy..."
go mod tidy

echo "Running go fmt..."
go fmt ./...

echo "Running go vet..."
go vet ./...

echo "Installing staticcheck if needed..."
command -v staticcheck >/dev/null 2>&1 || go install honnef.co/go/tools/cmd/staticcheck@latest

echo "Running staticcheck..."
staticcheck ./...

echo "Running tests..."
go test -v -race -cover ./...

echo "Building..."
go build -o mdpreview .

echo "✓ All checks passed!"
