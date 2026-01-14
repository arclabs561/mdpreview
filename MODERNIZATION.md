# Modernization Summary

This document outlines changes made to bring the mdpreview codebase up to Go 1.21-era standards.

## Version Updates

- **Go version**: 1.20 → 1.21
- **Dependencies**: Updated to stable versions
  - `fsnotify`: 1.6.0 → 1.7.0
  - `gorilla/mux`: 1.8.0 → 1.8.1
  - `gorilla/websocket`: 1.5.0 → 1.5.1
  - And transitive dependencies

## Critical Bug Fixes

### 1. Argument Parsing Bug (main.go)

**Before:**
```go
if len(os.Args) < 2 {
    log.Fatal("path must be given")
}
path := os.Args[1]  // BUG: Gets flag name, not file path!
```

**After:**
```go
args := flag.Args()
if len(args) < 1 {
    log.Fatal("markdown file path must be provided as an argument")
}
path := args[0]  // Correctly gets positional argument
```

### 2. Deprecated stdlib Usage (server/server.go)

**Before:**
```go
import "io/ioutil"
// ...
input, err := ioutil.ReadFile(s.path)  // Deprecated since Go 1.16
b, err := ioutil.ReadAll(response.Body)
```

**After:**
```go
import "io"
import "os"
// ...
input, err := os.ReadFile(s.path)  // Modern stdlib
b, err := io.ReadAll(response.Body)
```

### 3. Race Condition in File Watcher

**Before:**
```go
case fsnotify.Remove:
    w.Add(event.Name)  // Immediate re-add can race with editor saves
```

**After:**
```go
case fsnotify.Remove, fsnotify.Rename:
    // Delay to handle editor save patterns (write to temp, rename)
    go func() {
        time.Sleep(100 * time.Millisecond)
        if err := w.Add(s.path); err != nil {
            s.log.WithError(err).Debug("failed to re-add watch")
        }
    }()
```

### 4. Unreachable Code

**Before:**
```go
done := make(chan bool)
// ... goroutine never closes done
<-done  // Blocks forever!
```

**After:**
```go
// Proper context-based cancellation
for {
    select {
    case <-s.ctx.Done():
        return
    // ... other cases
    }
}
```

## Modern Go Patterns

### 1. Context Usage

Added `context.Context` throughout for cancellation and timeouts:

```go
// Server now accepts context
func New(ctx context.Context, path string, log *logrus.Logger, renderLocally bool) (*Server, error)

// HTTP requests use context
req, err := http.NewRequestWithContext(s.ctx, "POST", url, body)

// Goroutines respect context cancellation
case <-s.ctx.Done():
    return
```

### 2. Graceful Shutdown

```go
// Signal handling
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit

// Graceful shutdown with timeout
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
srv.Shutdown(shutdownCtx)
```

### 3. Proper HTTP Server Configuration

```go
srv := &http.Server{
    Addr:         *addr,
    Handler:      handler,
    ReadTimeout:  15 * time.Second,
    WriteTimeout: 15 * time.Second,
    IdleTimeout:  60 * time.Second,
}
```

### 4. Go 1.16+ embed Instead of go-bindata

```go
import "embed"

//go:embed static/*
var staticFiles embed.FS

staticFileHandler := http.FileServer(http.FS(staticFiles))
```

## Security Improvements

### 1. WebSocket Origin Checking

```go
upgrader: websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool {
        origin := r.Header.Get("Origin")
        return origin == "" || origin == "http://"+r.Host
    },
}
```

## Tooling Notes

This repo historically had make/check scripts; the current quick path is:

```sh
go install github.com/arclabs561/mdpreview@latest
```

## Migration Guide

If you previously installed from the old module path, reinstall:

```sh
go install github.com/arclabs561/mdpreview@latest
```
