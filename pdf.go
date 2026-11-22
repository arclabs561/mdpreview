package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/sirupsen/logrus"
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

	srv, addr, err := startPreviewServer(ctx, inputPath, log)
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}
	defer srv.Close()

	pageURL := "http://" + addr

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	chromeCtx, chromeCancel := chromedp.NewContext(allocCtx)
	defer chromeCancel()

	chromeCtx, timeoutCancel := context.WithTimeout(chromeCtx, 30*time.Second)
	defer timeoutCancel()

	err = chromedp.Do(chromeCtx,
		chromedp.Navigate(pageURL),
		chromedp.WaitReady(chromedp.CSS("#content")),
		chromedp.Sleep(1*time.Second),
		// Hide sidebar and header for clean PDF
		chromedp.Evaluate[chromedp.Void](`
			document.getElementById('sidebar').style.display = 'none';
			document.querySelector('.header').style.display = 'none';
			document.querySelector('.content-wrapper').style.padding = '0';
			document.querySelector('.markdown-body').style.maxWidth = 'none';
		`),
		chromedp.Sleep(200*time.Millisecond),
	)
	if err != nil {
		return fmt.Errorf("pdf: %w", err)
	}

	buf, err := chromedp.Run(chromeCtx, chromedp.PrintToPDF(chromedp.PDFPrintBackground(), chromedp.PDFPreferCSSPageSize(), chromedp.PDFMargin(0.5)))
	if err != nil {
		return fmt.Errorf("pdf: %w", err)
	}
	if err := os.WriteFile(outPath, buf, 0644); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	fmt.Println(outPath)
	return nil
}
