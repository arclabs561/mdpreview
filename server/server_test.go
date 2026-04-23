package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
)

func silentLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

func writeMarkdown(t *testing.T, dir, name, contents string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func newServerForFile(t *testing.T, contents string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := writeMarkdown(t, dir, "doc.md", contents)
	s, err := New(context.Background(), path, silentLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, path
}

func TestNew_File(t *testing.T) {
	s, path := newServerForFile(t, "# hi")
	if s.rootDir != filepath.Dir(path) {
		t.Errorf("rootDir: got %q, want %q", s.rootDir, filepath.Dir(path))
	}
	if s.defaultFile != "doc.md" {
		t.Errorf("defaultFile: got %q, want %q", s.defaultFile, "doc.md")
	}
}

func TestNew_DirectoryFindsReadme(t *testing.T) {
	dir := t.TempDir()
	writeMarkdown(t, dir, "README.md", "# readme")
	writeMarkdown(t, dir, "other.md", "# other")

	s, err := New(context.Background(), dir, silentLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.rootDir != dir {
		t.Errorf("rootDir: got %q, want %q", s.rootDir, dir)
	}
	if s.defaultFile != "README.md" {
		t.Errorf("defaultFile: got %q, want README.md", s.defaultFile)
	}
}

func TestNew_DirectoryWithoutReadme(t *testing.T) {
	dir := t.TempDir()
	writeMarkdown(t, dir, "notes.md", "# notes")

	s, err := New(context.Background(), dir, silentLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.defaultFile != "" {
		t.Errorf("defaultFile: got %q, want empty", s.defaultFile)
	}
}

func TestNew_NonexistentPath(t *testing.T) {
	if _, err := New(context.Background(), "/definitely/does/not/exist.md", silentLogger()); err == nil {
		t.Error("expected error for missing path")
	}
}

// findReadme is filesystem-dependent. macOS/APFS is case-insensitive, so
// we can't test the lowercase/titlecase precedence portably. Test only the
// portable behavior: README.md is found, absence returns empty.
func TestFindReadme(t *testing.T) {
	t.Run("README.md present", func(t *testing.T) {
		dir := t.TempDir()
		writeMarkdown(t, dir, "README.md", "x")
		if got := findReadme(dir); got != "README.md" {
			t.Errorf("got %q, want README.md", got)
		}
	})
	t.Run("no readme", func(t *testing.T) {
		dir := t.TempDir()
		writeMarkdown(t, dir, "other.md", "x")
		if got := findReadme(dir); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
}

// resolveFile is security-critical: it guards every handler that reads a
// file. Traversal regressions would expose the filesystem.
//
// Note: filepath.Join("/rootDir", "/etc/passwd") yields "/rootDir/etc/passwd"
// -- Go folds an absolute argument into the joined path. So an absolute-path
// attack does not escape rootDir (the resulting path just won't exist). Only
// "../" sequences that actually climb past rootDir are rejected here.
func TestResolveFile_BlocksParentTraversal(t *testing.T) {
	dir := t.TempDir()
	writeMarkdown(t, dir, "README.md", "# r")
	outside := t.TempDir()
	writeMarkdown(t, outside, "secret.md", "SECRET")

	s, err := New(context.Background(), filepath.Join(dir, "README.md"), silentLogger())
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{
		"../" + filepath.Base(outside) + "/secret.md",
		"../../etc/passwd",
	} {
		t.Run(target, func(t *testing.T) {
			abs, err := s.resolveFile(target)
			if err == nil {
				t.Errorf("expected error, got abs=%q", abs)
			}
		})
	}
}

func TestResolveFile_EmptyUsesDefault(t *testing.T) {
	s, path := newServerForFile(t, "# hi")
	abs, err := s.resolveFile("")
	if err != nil {
		t.Fatal(err)
	}
	if abs != path {
		t.Errorf("resolveFile(\"\"): got %q, want %q", abs, path)
	}
}

func TestResolveFile_Within(t *testing.T) {
	dir := t.TempDir()
	writeMarkdown(t, dir, "sub/doc.md", "# d")

	s, err := New(context.Background(), filepath.Join(dir, "sub", "doc.md"), silentLogger())
	if err != nil {
		t.Fatal(err)
	}
	// rootDir is .../sub, so "doc.md" resolves within.
	abs, err := s.resolveFile("doc.md")
	if err != nil {
		t.Fatal(err)
	}
	if abs != filepath.Join(dir, "sub", "doc.md") {
		t.Errorf("resolveFile: got %q", abs)
	}
}

func TestIsBinary(t *testing.T) {
	for _, c := range []struct {
		name string
		data []byte
		want bool
	}{
		{"empty", nil, false},
		{"plain text", []byte("hello world\n"), false},
		{"utf8", []byte("héllo ☃"), false},
		{"null byte", []byte{'h', 0, 'i'}, true},
		{"null after 512", append(bytes.Repeat([]byte("a"), 512), 0), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := isBinary(c.data); got != c.want {
				t.Errorf("isBinary: got %v, want %v", got, c.want)
			}
		})
	}
}

func TestBuildTreeFromPaths_Structure(t *testing.T) {
	files := []string{"a/b.md", "a/c.md", "top.md"}
	status := map[string]string{"a/b.md": "M"}

	tree := buildTreeFromPaths(files, status)
	if len(tree) != 2 {
		t.Fatalf("want 2 top-level entries, got %d", len(tree))
	}
	// Directories sort before files.
	if !tree[0].IsDir || tree[0].Name != "a" {
		t.Errorf("entry 0: want dir 'a', got %+v", tree[0])
	}
	if tree[1].Name != "top.md" || tree[1].IsDir {
		t.Errorf("entry 1: want file 'top.md', got %+v", tree[1])
	}
	// Status propagates from a/b.md up to the a/ directory.
	if tree[0].Status != "M" {
		t.Errorf("dir a status: got %q, want M", tree[0].Status)
	}
	// Leaf file status from statusMap is preserved.
	var bm *TreeEntry
	for i := range tree[0].Children {
		if tree[0].Children[i].Name == "b.md" {
			bm = &tree[0].Children[i]
		}
	}
	if bm == nil || bm.Status != "M" {
		t.Errorf("a/b.md: got %+v", bm)
	}
}

func TestAnnotateSourceLines(t *testing.T) {
	source := []byte("# first\n\n## second\n\npara\n")
	html := []byte("<h1>first</h1>\n<h2>second</h2>\n<p>para</p>\n")
	out := annotateSourceLines(source, html)
	// Blocks start at lines 1 (# first), 3 (## second), 5 (para).
	for _, want := range []string{
		`<h1 data-source-line="1"`,
		`<h2 data-source-line="3"`,
		`<p data-source-line="5"`,
	} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestHandleIndex(t *testing.T) {
	s, path := newServerForFile(t, "# hi")
	h, err := s.Run()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type: got %q", ct)
	}
	body := rr.Body.String()
	// The template is Shiki-based and includes repoName/path; the file name
	// should land somewhere in the rendered page.
	if !strings.Contains(body, filepath.Base(path)) {
		t.Errorf("body missing filename %q", filepath.Base(path))
	}
}

func TestHandleRaw_ServesText(t *testing.T) {
	s, path := newServerForFile(t, "# hello")
	h, err := s.Run()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/raw?file="+filepath.Base(path), nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%q", rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); got != "# hello" {
		t.Errorf("body: got %q, want %q", got, "# hello")
	}
}

func TestHandleRaw_RejectsBinary(t *testing.T) {
	dir := t.TempDir()
	writeMarkdown(t, dir, "README.md", "# r")
	binPath := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(binPath, []byte{'x', 0, 'y'}, 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := New(context.Background(), dir, silentLogger())
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.Run()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/raw?file=blob.bin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status: got %d, want 415", rr.Code)
	}
}

func TestHandleRaw_RejectsTraversal(t *testing.T) {
	s, _ := newServerForFile(t, "# hi")
	h, err := s.Run()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/raw?file=../../../etc/passwd", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rr.Code)
	}
}

func TestHandleMeta(t *testing.T) {
	s, path := newServerForFile(t, "# hi")
	h, err := s.Run()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/meta?file="+filepath.Base(path), nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if _, ok := resp["modTime"]; !ok {
		t.Errorf("response missing modTime: %v", resp)
	}
}

func TestHandleTree_NonGitDirWalks(t *testing.T) {
	dir := t.TempDir()
	writeMarkdown(t, dir, "a.md", "# a")
	writeMarkdown(t, dir, "sub/b.md", "# b")

	s, err := New(context.Background(), dir, silentLogger())
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.Run()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/tree", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var tree []TreeEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &tree); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	var walk func([]TreeEntry)
	walk = func(es []TreeEntry) {
		for _, e := range es {
			names[e.Name] = true
			walk(e.Children)
		}
	}
	walk(tree)
	for _, n := range []string{"a.md", "sub", "b.md"} {
		if !names[n] {
			t.Errorf("tree missing %q: %+v", n, tree)
		}
	}
}

func TestCheckOrigin(t *testing.T) {
	s, _ := newServerForFile(t, "# hi")
	for _, c := range []struct {
		name   string
		origin string
		host   string
		want   bool
	}{
		{"empty allowed", "", "localhost:8080", true},
		{"http same host", "http://localhost:8080", "localhost:8080", true},
		{"https same host", "https://localhost:8080", "localhost:8080", true},
		{"http different host", "http://evil.example:8080", "localhost:8080", false},
		{"https different host", "https://evil.example", "localhost:8080", false},
		{"malformed no scheme", "localhost", "localhost:8080", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/ws", nil)
			r.Header.Set("Origin", c.origin)
			r.Host = c.host
			if got := s.upgrader.CheckOrigin(r); got != c.want {
				t.Errorf("CheckOrigin(%q, host=%q): got %v, want %v", c.origin, c.host, got, c.want)
			}
		})
	}
}

// End-to-end: dial the real WebSocket route, receive the initial render,
// modify the source file, receive the updated render. Exercises the upgrade,
// watcher (directory-level), debounce, render, and writer chain.
func TestWebSocket_InitialAndUpdatedRender(t *testing.T) {
	dir := t.TempDir()
	path := writeMarkdown(t, dir, "doc.md", "# first")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := New(ctx, path, silentLogger())
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.Run()
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?file=doc.md"
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	readText := func(deadline time.Duration) []byte {
		t.Helper()
		if err := c.SetReadDeadline(time.Now().Add(deadline)); err != nil {
			t.Fatal(err)
		}
		for {
			mt, msg, err := c.ReadMessage()
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if mt == websocket.TextMessage {
				return msg
			}
			// Skip pings/pongs.
		}
	}

	msg := readText(3 * time.Second)
	if !bytes.Contains(msg, []byte("first")) {
		t.Fatalf("initial render missing 'first': %s", msg)
	}

	if err := os.WriteFile(path, []byte("# second"), 0o600); err != nil {
		t.Fatal(err)
	}

	msg = readText(3 * time.Second)
	if !bytes.Contains(msg, []byte("second")) {
		t.Errorf("updated render missing 'second': %s", msg)
	}
}

// Foreign-Origin handshakes must be rejected by the upgrader's CheckOrigin.
func TestWebSocket_RejectsForeignOrigin(t *testing.T) {
	dir := t.TempDir()
	writeMarkdown(t, dir, "doc.md", "# hi")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := New(ctx, filepath.Join(dir, "doc.md"), silentLogger())
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.Run()
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?file=doc.md"
	hdr := http.Header{"Origin": []string{"http://evil.example"}}
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, hdr)
	if err == nil {
		t.Fatal("expected dial error for foreign origin")
	}
	if resp != nil && resp.StatusCode != http.StatusForbidden {
		t.Errorf("status: got %d, want 403", resp.StatusCode)
	}
}
