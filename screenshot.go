package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/sirupsen/logrus"
	"github.com/urfave/negroni"

	negronilogrus "github.com/meatballhat/negroni-logrus"

	"github.com/arclabs561/mdpreview/server"
)

func runScreenshot(args []string) error {
	fs := flag.NewFlagSet("screenshot", flag.ExitOnError)
	output := fs.String("o", "", "output file (default: <input>.png)")
	width := fs.Int("width", 980, "viewport width in pixels")
	dark := fs.Bool("dark", false, "force dark color scheme")
	fs.Parse(args)

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: mdpreview screenshot [-o output.png] <file.md>")
	}
	inputPath := fs.Arg(0)

	// Default output name
	outPath := *output
	if outPath == "" {
		ext := filepath.Ext(inputPath)
		outPath = strings.TrimSuffix(inputPath, ext) + ".png"
	}

	log := logrus.New()
	log.SetLevel(logrus.WarnLevel)

	// Start server on random port
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := server.New(ctx, inputPath, log)
	if err != nil {
		return fmt.Errorf("server init: %w", err)
	}
	h, err := s.Run()
	if err != nil {
		return fmt.Errorf("server run: %w", err)
	}

	n := negroni.New()
	n.Use(negroni.NewRecovery())
	n.Use(negronilogrus.NewMiddlewareFromLogger(log, "web"))
	n.UseHandler(h)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	srv := &http.Server{Handler: n}
	go srv.Serve(listener)
	defer srv.Close()

	addr := listener.Addr().String()
	pageURL := fmt.Sprintf("http://%s", addr)

	// Chrome options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.WindowSize(*width, 800),
		chromedp.Flag("headless", true),
	)
	if *dark {
		opts = append(opts, chromedp.Flag("force-dark-mode", true))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	chromeCtx, chromeCancel := chromedp.NewContext(allocCtx)
	defer chromeCancel()

	chromeCtx, timeoutCancel := context.WithTimeout(chromeCtx, 30*time.Second)
	defer timeoutCancel()

	var buf []byte
	err = chromedp.Run(chromeCtx,
		chromedp.EmulateViewport(int64(*width), 800),
		chromedp.Navigate(pageURL),
		// Wait for content to render (shiki, katex, etc.)
		chromedp.WaitReady("#content", chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		// Resize viewport to full content height, then screenshot
		chromedp.ActionFunc(func(ctx context.Context) error {
			var height float64
			if err := chromedp.Evaluate(`document.querySelector('.content-wrapper').scrollHeight`, &height).Do(ctx); err != nil {
				return err
			}
			h := max(int64(math.Ceil(height)), 800) + 48 // +header
			return chromedp.EmulateViewport(int64(*width), h).Do(ctx)
		}),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.FullScreenshot(&buf, 90),
	)
	if err != nil {
		return fmt.Errorf("screenshot: %w", err)
	}

	if err := os.WriteFile(outPath, buf, 0644); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	fmt.Println(outPath)
	return nil
}
