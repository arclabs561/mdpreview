package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
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
	output := fs.String("o", "", "output file or directory")
	width := fs.Int("width", 980, "viewport width in pixels")
	dark := fs.Bool("dark", false, "force dark color scheme")
	concat := fs.Bool("concat", false, "concatenate all pages into one image")
	fs.Parse(args)

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: mdpreview screenshot [-o output] [-concat] <file.md | directory>")
	}
	inputPath := fs.Arg(0)

	info, err := os.Stat(inputPath)
	if err != nil {
		return err
	}

	log := logrus.New()
	log.SetLevel(logrus.WarnLevel)

	// Determine the server root and which files to screenshot
	var serverPath string
	var mdFiles []string

	if info.IsDir() {
		serverPath = inputPath
		// mdFiles populated after server starts (needs /api/tree)
	} else {
		serverPath = inputPath
		// For single file, the relative path within rootDir is just the filename
		mdFiles = []string{filepath.Base(inputPath)}
	}

	// Start server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := server.New(ctx, serverPath, log)
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

	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())

	// For directories, discover .md files via the server's tree API
	if info.IsDir() {
		mdFiles, err = findMarkdownFilesFromServer(baseURL)
		if err != nil {
			return fmt.Errorf("listing files: %w", err)
		}
		if len(mdFiles) == 0 {
			return fmt.Errorf("no .md files found in %s", inputPath)
		}
	}

	// Chrome allocator (shared across all screenshots)
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

	timeout := time.Duration(max(30, len(mdFiles)*15)) * time.Second
	chromeCtx, timeoutCancel := context.WithTimeout(chromeCtx, timeout)
	defer timeoutCancel()

	if *concat || !info.IsDir() {
		// Single output: one file or concatenated
		images, err := screenshotPages(chromeCtx, baseURL, mdFiles, *width)
		if err != nil {
			return err
		}

		outPath := *output
		if outPath == "" {
			if info.IsDir() {
				outPath = filepath.Base(inputPath) + ".png"
			} else {
				ext := filepath.Ext(inputPath)
				outPath = strings.TrimSuffix(inputPath, ext) + ".png"
			}
		}

		var finalBuf []byte
		if len(images) == 1 {
			finalBuf = images[0]
		} else {
			finalBuf, err = concatImages(images)
			if err != nil {
				return fmt.Errorf("concat: %w", err)
			}
		}

		if err := os.WriteFile(outPath, finalBuf, 0644); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		fmt.Println(outPath)
	} else {
		// Separate files
		outDir := *output
		if outDir == "" {
			outDir = "."
		}
		for _, file := range mdFiles {
			images, err := screenshotPages(chromeCtx, baseURL, []string{file}, *width)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: %s: %v\n", file, err)
				continue
			}
			name := strings.TrimSuffix(file, filepath.Ext(file)) + ".png"
			outPath := filepath.Join(outDir, name)
			os.MkdirAll(filepath.Dir(outPath), 0755)
			if err := os.WriteFile(outPath, images[0], 0644); err != nil {
				fmt.Fprintf(os.Stderr, "warning: %s: %v\n", outPath, err)
				continue
			}
			fmt.Println(outPath)
		}
	}
	return nil
}


func screenshotPages(ctx context.Context, baseURL string, files []string, width int) ([][]byte, error) {
	var results [][]byte

	for _, file := range files {
		pageURL := fmt.Sprintf("%s/?file=%s", baseURL, file)
		var buf []byte
		err := chromedp.Run(ctx,
			chromedp.EmulateViewport(int64(width), 800),
			chromedp.Navigate(pageURL),
			chromedp.WaitReady("#content", chromedp.ByID),
			chromedp.Sleep(500*time.Millisecond),
			// Resize to full content height
			chromedp.ActionFunc(func(ctx context.Context) error {
				var height float64
				if err := chromedp.Evaluate(`document.querySelector('.content-wrapper').scrollHeight`, &height).Do(ctx); err != nil {
					return err
				}
				h := max(int64(math.Ceil(height)), 800) + 48
				return chromedp.EmulateViewport(int64(width), h).Do(ctx)
			}),
			chromedp.Sleep(100*time.Millisecond),
			chromedp.FullScreenshot(&buf, 90),
		)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		results = append(results, buf)
	}
	return results, nil
}

func concatImages(pngs [][]byte) ([]byte, error) {
	var images []image.Image
	totalHeight := 0
	maxWidth := 0

	for _, data := range pngs {
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		images = append(images, img)
		b := img.Bounds()
		totalHeight += b.Dy()
		if b.Dx() > maxWidth {
			maxWidth = b.Dx()
		}
	}

	canvas := image.NewRGBA(image.Rect(0, 0, maxWidth, totalHeight))
	y := 0
	for _, img := range images {
		b := img.Bounds()
		draw.Draw(canvas, image.Rect(0, y, b.Dx(), y+b.Dy()), img, b.Min, draw.Src)
		y += b.Dy()
	}

	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// findMarkdownFilesFromServer fetches the tree API and extracts .md file paths.
func findMarkdownFilesFromServer(baseURL string) ([]string, error) {
	resp, err := http.Get(baseURL + "/api/tree")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	type entry struct {
		Name     string  `json:"name"`
		Path     string  `json:"path"`
		IsDir    bool    `json:"isDir"`
		Children []entry `json:"children,omitempty"`
	}

	var tree []entry
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, err
	}

	var files []string
	var walk func(entries []entry)
	walk = func(entries []entry) {
		for _, e := range entries {
			if e.IsDir {
				walk(e.Children)
			} else if strings.HasSuffix(strings.ToLower(e.Path), ".md") {
				files = append(files, e.Path)
			}
		}
	}
	walk(tree)
	return files, nil
}
