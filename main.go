package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	negronilogrus "github.com/meatballhat/negroni-logrus"
	"github.com/sirupsen/logrus"
	"github.com/urfave/negroni"

	"github.com/arclabs561/mdpreview/server"
)

var (
	addr  = flag.String("addr", ":8080", "address to serve preview like :8080 or 0.0.0.0:7000")
	api   = flag.Bool("api", false, "whether to render via the Github API")
	debug = flag.Bool("debug", false, "debug logging")
)

func main() {
	flag.Parse()

	log := logrus.New()
	if *debug {
		log.SetLevel(logrus.DebugLevel)
	}

	// Fix: Use flag.Args() instead of os.Args after flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		log.Fatal("markdown file path must be provided as an argument")
	}
	path := args[0]

	if filepath.Ext(path) != ".md" {
		log.Warnf("path %s doesn't look like a Markdown file", path)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Fatalf("path %s does not exist", path)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := server.New(ctx, path, log, !*api)
	if err != nil {
		log.Fatal(err)
	}
	h, err := s.Run()
	if err != nil {
		log.Fatal(err)
	}

	if strings.HasPrefix(*addr, ":") {
		*addr = fmt.Sprintf("127.0.0.1%s", *addr)
	}

	// Setup HTTP server with timeouts
	srv := &http.Server{
		Addr:         *addr,
		Handler:      createHandler(h, log),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Infof("Starting mdpreview server at http://%s", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down server...")
	cancel() // Cancel context to signal goroutines

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Errorf("Server forced to shutdown: %v", err)
	}

	log.Info("Server stopped")
}

func createHandler(h http.Handler, log *logrus.Logger) http.Handler {
	n := negroni.New()
	n.Use(negroni.NewRecovery())
	n.Use(negronilogrus.NewMiddlewareFromLogger(log, "web"))
	n.UseHandler(h)
	return n
}
