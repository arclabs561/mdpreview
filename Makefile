.PHONY: all build install test clean client

all: client build

build:
	go build -o mdpreview .

install: client
	go install .

test:
	go test -v -race -cover ./...

clean:
	rm -f mdpreview
	rm -f server/static/bundle.js
	go clean

# Build the client JS bundle (requires bun or npm in client/)
client:
	cd client && bun install --frozen-lockfile 2>/dev/null || cd client && npm ci
	cd client && bun run build
