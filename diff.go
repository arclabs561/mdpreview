package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/sirupsen/logrus"
	"github.com/urfave/negroni"

	negronilogrus "github.com/meatballhat/negroni-logrus"

	"github.com/arclabs561/mdpreview/server"
)

func runDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	output := fs.String("o", "", "save screenshot to file instead of opening browser")
	width := fs.Int("width", 980, "viewport width")
	dark := fs.Bool("dark", false, "force dark mode")
	light := fs.Bool("light", false, "force light mode")
	fs.Parse(args)

	if fs.NArg() < 1 {
		return fmt.Errorf("usage: mdpreview diff [-o output.png] <file.md>")
	}
	filePath := fs.Arg(0)

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return err
	}

	// Get the HEAD version of the file
	dir := filepath.Dir(absPath)
	relPath, err := gitRelPath(dir, absPath)
	if err != nil {
		return fmt.Errorf("not in a git repo: %w", err)
	}

	headContent, err := gitShowHead(dir, relPath)
	if err != nil {
		return fmt.Errorf("no HEAD version (new file?): %w", err)
	}

	// Write HEAD version to a temp file alongside the current file
	tmpDir, err := os.MkdirTemp("", "mdpreview-diff-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// Create two files: old.md and new.md
	oldPath := filepath.Join(tmpDir, "old.md")
	newPath := filepath.Join(tmpDir, "new.md")
	if err := os.WriteFile(oldPath, headContent, 0644); err != nil {
		return err
	}
	currentContent, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}
	if err := os.WriteFile(newPath, currentContent, 0644); err != nil {
		return err
	}

	diffHTML := buildDiffHTML(filePath)

	log := logrus.New()
	log.SetLevel(logrus.WarnLevel)

	// Start two servers: one for old, one for new
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	oldServer, oldAddr, err := startPreviewServer(ctx, oldPath, log)
	if err != nil {
		return fmt.Errorf("old server: %w", err)
	}
	defer oldServer.Close()

	newServer, newAddr, err := startPreviewServer(ctx, newPath, log)
	if err != nil {
		return fmt.Errorf("new server: %w", err)
	}
	defer newServer.Close()

	// Serve the diff viewer page
	diffMux := http.NewServeMux()
	diffMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		html := strings.ReplaceAll(diffHTML, "OLD_URL", "http://"+oldAddr)
		html = strings.ReplaceAll(html, "NEW_URL", "http://"+newAddr)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(html))
	})

	diffListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer diffListener.Close()

	diffSrv := &http.Server{Handler: diffMux}
	go diffSrv.Serve(diffListener)
	defer diffSrv.Close()

	diffURL := "http://" + diffListener.Addr().String()

	if *output == "" {
		// Open in browser
		fmt.Printf("Diff: %s\n", diffURL)
		fmt.Println("  Old (HEAD): http://" + oldAddr)
		fmt.Println("  New (working): http://" + newAddr)
		openBrowser(diffURL)

		// Wait for interrupt
		<-ctx.Done()
		return nil
	}

	// Screenshot mode
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.WindowSize(*width, 800),
		chromedp.Flag("headless", true),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	chromeCtx, chromeCancel := chromedp.NewContext(allocCtx)
	defer chromeCancel()

	chromeCtx, timeoutCancel := context.WithTimeout(chromeCtx, 30*time.Second)
	defer timeoutCancel()

	var buf []byte
	actions := []chromedp.Action{
		chromedp.EmulateViewport(int64(*width), 800),
	}
	if *dark || *light {
		scheme := "dark"
		if *light {
			scheme = "light"
		}
		actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
			return emulation.SetEmulatedMedia().
				WithFeatures([]*emulation.MediaFeature{
					{Name: "prefers-color-scheme", Value: scheme},
				}).Do(ctx)
		}))
	}
	actions = append(actions,
		chromedp.Navigate(diffURL),
		chromedp.Sleep(2*time.Second),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var height float64
			if err := chromedp.Evaluate(`document.body.scrollHeight`, &height).Do(ctx); err != nil {
				return err
			}
			h := max(int64(math.Ceil(height)), 800)
			return chromedp.EmulateViewport(int64(*width), h).Do(ctx)
		}),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.FullScreenshot(&buf, 90),
	)

	if err := chromedp.Run(chromeCtx, actions...); err != nil {
		return fmt.Errorf("screenshot: %w", err)
	}

	if err := os.WriteFile(*output, buf, 0644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	fmt.Println(*output)
	return nil
}

func startPreviewServer(ctx context.Context, path string, log *logrus.Logger) (*http.Server, string, error) {
	s, err := server.New(ctx, path, log)
	if err != nil {
		return nil, "", err
	}
	h, err := s.Run()
	if err != nil {
		return nil, "", err
	}

	n := negroni.New()
	n.Use(negroni.NewRecovery())
	n.Use(negronilogrus.NewMiddlewareFromLogger(log, "web"))
	n.UseHandler(h)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}

	srv := &http.Server{Handler: n}
	go srv.Serve(listener)

	return srv, listener.Addr().String(), nil
}

func gitRelPath(dir, absPath string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return "", err
	}
	return rel, nil
}

func gitShowHead(dir, relPath string) ([]byte, error) {
	cmd := exec.Command("git", "show", "HEAD:"+relPath)
	cmd.Dir = dir
	return cmd.Output()
}

func buildDiffHTML(filename string) string {
	name := strings.ReplaceAll(filepath.Base(filename), "<", "&lt;")
	return `<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<title>diff: ` + name + `</title>
<style>
  :root {
    --bg: #ffffff; --fg: #1f2328; --border: #d1d9e0;
    --bg-old: #fff5f5; --bg-new: #f0fff4;
    --label-old: #d1242f; --label-new: #1a7f37;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #0d1117; --fg: #f0f6fc; --border: #3d444d;
      --bg-old: #1c0c0c; --bg-new: #0c1c0c;
      --label-old: #f85149; --label-new: #3fb950;
    }
  }
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: -apple-system, sans-serif; background: var(--bg); color: var(--fg); }
  .header {
    padding: 12px 20px;
    border-bottom: 1px solid var(--border);
    font-size: 14px;
    font-weight: 600;
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .header .filename { flex: 1; }
  .label { font-size: 11px; font-weight: 600; padding: 2px 8px; border-radius: 12px; }
  .label-old { color: var(--label-old); border: 1px solid var(--label-old); }
  .label-new { color: var(--label-new); border: 1px solid var(--label-new); }
  .diff-container {
    display: flex;
    height: calc(100vh - 45px);
  }
  .diff-pane {
    flex: 1;
    border-right: 1px solid var(--border);
    position: relative;
  }
  .diff-pane:last-child { border-right: none; }
  .pane-label {
    position: absolute;
    top: 8px;
    right: 12px;
    z-index: 10;
  }
  .diff-pane iframe {
    width: 100%;
    height: 100%;
    border: none;
  }
</style>
</head>
<body>
  <div class="header">
    <span class="filename">` + name + `</span>
    <span class="label label-old">HEAD</span>
    <span>vs</span>
    <span class="label label-new">Working</span>
  </div>
  <div class="diff-container">
    <div class="diff-pane">
      <div class="pane-label"><span class="label label-old">HEAD</span></div>
      <iframe src="OLD_URL"></iframe>
    </div>
    <div class="diff-pane">
      <div class="pane-label"><span class="label label-new">Working</span></div>
      <iframe src="NEW_URL"></iframe>
    </div>
  </div>
</body>
</html>`
}
