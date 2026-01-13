.PHONY: all build install test clean css

all: build

build:
	go build -o mdpreview .

install:
	go install .

test:
	go test -v -race -cover ./...

clean:
	rm -f mdpreview
	go clean

# Update GitHub CSS styling
css:
	@command -v generate-github-markdown-css >/dev/null 2>&1 || npm install --global generate-github-markdown-css
	generate-github-markdown-css > server/static/github.css
	@command -v minify >/dev/null 2>&1 || go install github.com/tdewolff/minify/v2/cmd/minify@latest
	minify -o server/static/github.css server/static/github.css

# Run linters and formatters
lint:
	go fmt ./...
	go vet ./...
	@command -v staticcheck >/dev/null 2>&1 || go install honnef.co/go/tools/cmd/staticcheck@latest
	staticcheck ./...
	@command -v golangci-lint >/dev/null 2>&1 || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	golangci-lint run
