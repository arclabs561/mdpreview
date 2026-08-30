.PHONY: all build install test e2e clean client

all: client build

build:
	go build -o mdpreview .

install: client
	go install .

test:
	go test -v -race -cover ./...

e2e: client
	cd client && bun run test:e2e

clean:
	rm -f mdpreview
	rm -f server/static/bundle.js
	go clean

# Build the client JS bundle (requires bun or npm in client/)
client:
	cd client && (bun install --frozen-lockfile 2>/dev/null || npm install)
	cd client && (bun run build || npm run build)
