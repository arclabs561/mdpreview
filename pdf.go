package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/sirupsen/logrus"
	"github.com/urfave/negroni"

	negronilogrus "github.com/meatballhat/negroni-logrus"

	"github.com/arclabs561/mdpreview/server"
)

func runPDF(args []string) error {
	fs := flag.NewFlagSet("pdf", flag.ExitOnError)
	output := fs.String("o", "", "output file (default: <input>.pdf)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: mdpreview pdf [-o output.pdf] <file.md>")
	}
	inputPath := fs.Arg(0)

	outPath := *output
	if outPath == "" {
		ext := filepath.Ext(inputPath)
		outPath = strings.TrimSuffix(inputPath, ext) + ".pdf"
	}

	log := logrus.New()
	log.SetLevel(logrus.WarnLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

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

	pageURL := fmt.Sprintf("http://%s", listener.Addr().String())

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	chromeCtx, chromeCancel := chromedp.NewContext(allocCtx)
	defer chromeCancel()

	chromeCtx, timeoutCancel := context.WithTimeout(chromeCtx, 30*time.Second)
	defer timeoutCancel()

	var buf []byte
	err = chromedp.Run(chromeCtx,
		chromedp.Navigate(pageURL),
		chromedp.WaitReady("#content", chromedp.ByID),
		chromedp.Sleep(1*time.Second),
		// Hide sidebar and header for clean PDF
		chromedp.Evaluate(`
			document.getElementById('sidebar').style.display = 'none';
			document.querySelector('.header').style.display = 'none';
			document.querySelector('.content-wrapper').style.padding = '0';
			document.querySelector('.markdown-body').style.maxWidth = 'none';
		`, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			buf, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithPreferCSSPageSize(true).
				WithMarginTop(0.5).
				WithMarginBottom(0.5).
				WithMarginLeft(0.5).
				WithMarginRight(0.5).
				Do(ctx)
			return err
		}),
	)
	if err != nil {
		return fmt.Errorf("pdf: %w", err)
	}

	if err := os.WriteFile(outPath, buf, 0644); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	fmt.Println(outPath)
	return nil
}
