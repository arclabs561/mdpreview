package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	negronilogrus "github.com/meatballhat/negroni-logrus"
	"github.com/sirupsen/logrus"
	"github.com/urfave/negroni"

	"github.com/henrywallace/mdpreview/server"
)

var (
	addr  = flag.String("addr", ":8080", "address to serve preview like :8080 or 0.0.0.0:7000")
	debug = flag.Bool("debug", false, "debug logging")
)

func main() {
	flag.Parse()

	log := logrus.New()
	if *debug {
		log.SetLevel(logrus.DebugLevel)
	}

	args := flag.Args()
	if len(args) < 1 {
		log.Fatal("usage: mdpreview <file.md | directory>")
	}
	path := args[0]

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := server.New(ctx, path, log)
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

	srv := &http.Server{
		Addr:        *addr,
		Handler:     createHandler(h, log),
		ReadTimeout: 15 * time.Second,
		// No WriteTimeout: WebSocket connections are long-lived
		IdleTimeout:  60 * time.Second,
	}

	// Start server and open browser
	url := fmt.Sprintf("http://%s", *addr)
	go func() {
		log.Infof("Starting mdpreview server at %s", url)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Give the server a moment to start, then open browser
	go func() {
		time.Sleep(200 * time.Millisecond)
		openBrowser(url)
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

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	cmd.Run()
}

func createHandler(h http.Handler, log *logrus.Logger) http.Handler {
	n := negroni.New()
	n.Use(negroni.NewRecovery())
	n.Use(negronilogrus.NewMiddlewareFromLogger(log, "web"))
	n.UseHandler(h)
	return n
}
