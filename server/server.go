package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/yuin/goldmark"
	emoji "github.com/yuin/goldmark-emoji"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

const maxDocumentSize = 1 << 20

var (
	errDocumentTooLarge = errors.New("document too large")
	errBinaryDocument   = errors.New("binary document")
)

//go:embed static/*
var staticFiles embed.FS

// Server serves a HTML rendered Markdown preview. It can serve a single file
// or a directory (with file selection via ?file= query parameter).
type Server struct {
	ctx           context.Context
	rootDir       string // directory containing the markdown file(s)
	rootReal      string // evaluated rootDir, used for symlink-safe containment
	defaultFile   string // relative path to default file within rootDir
	indexTemplate *template.Template
	upgrader      websocket.Upgrader
	log           *logrus.Logger
	md            goldmark.Markdown
}

// New creates a new Server. path can be a file or directory.
func New(ctx context.Context, path string, log *logrus.Logger) (*Server, error) {
	indexData, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		return nil, err
	}

	indexTemplate, err := template.New("index").Parse(string(indexData))
	if err != nil {
		return nil, err
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Footnote, emoji.Emoji),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)

	// Determine rootDir and defaultFile
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	var rootDir, defaultFile string
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		rootDir = absPath
		defaultFile = findReadme(absPath)
	} else {
		rootDir = filepath.Dir(absPath)
		defaultFile, _ = filepath.Rel(rootDir, absPath)
	}
	rootReal, err := filepath.EvalSymlinks(rootDir)
	if err != nil {
		return nil, err
	}

	return &Server{
		ctx:           ctx,
		rootDir:       rootDir,
		rootReal:      rootReal,
		defaultFile:   defaultFile,
		log:           log,
		indexTemplate: indexTemplate,
		md:            md,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true
				}
				u, err := url.Parse(origin)
				if err != nil {
					return false
				}
				return u.Host == r.Host
			},
		},
	}, nil
}

func findReadme(dir string) string {
	for _, name := range []string{"README.md", "readme.md", "Readme.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return name
		}
	}
	return ""
}

// resolveFile returns an evaluated absolute path within rootDir. Evaluating
// symlinks prevents an in-tree link from escaping the served directory.
func (s *Server) resolveFile(file string) (string, error) {
	if file == "" {
		file = s.defaultFile
	}
	// Clean and resolve
	clean := filepath.Clean(file)
	abs := filepath.Join(s.rootDir, clean)
	// Ensure it's within rootDir (check with separator to prevent /foo matching /foobar)
	if abs != s.rootDir && !strings.HasPrefix(abs, s.rootDir+string(filepath.Separator)) {
		return "", os.ErrPermission
	}
	realPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	if realPath != s.rootReal && !strings.HasPrefix(realPath, s.rootReal+string(filepath.Separator)) {
		return "", os.ErrPermission
	}
	return realPath, nil
}

// Run returns handlers to run the server.
func (s *Server) Run() (http.Handler, error) {
	return s.setupHandlers(), nil
}

func (s *Server) setupHandlers() http.Handler {
	staticFileHandler := http.FileServer(http.FS(staticFiles))
	r := mux.NewRouter()
	r.HandleFunc("/", s.handleIndex).Methods("GET")
	r.HandleFunc("/api/tree", s.handleTree).Methods("GET")
	r.HandleFunc("/api/meta", s.handleMeta).Methods("GET")
	r.HandleFunc("/api/raw", s.handleRaw).Methods("GET")
	r.HandleFunc("/api/render", s.handleRender).Methods("POST")
	r.HandleFunc("/api/save", s.handleSave).Methods("PUT")
	r.HandleFunc("/api/diff", s.handleDiff).Methods("GET")
	r.HandleFunc("/ws", s.handleWebSocket).Methods("GET")
	r.PathPrefix("/files/").HandlerFunc(s.handleLocalFile).Methods("GET")
	r.PathPrefix("/").Handler(staticFileHandler).Methods("GET")

	return r
}

// TreeEntry represents a file or directory in the tree.
type TreeEntry struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"isDir"`
	Status   string      `json:"status,omitempty"` // M=modified, A=added, ?=untracked, D=deleted
	Children []TreeEntry `json:"children,omitempty"`
}

func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	files := s.gitTrackedFiles()
	statusMap := s.gitStatus()
	tree := buildTreeFromPaths(files, statusMap)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tree)
}

// gitTrackedFiles returns all tracked files (git ls-files), falling back
// to a directory walk if not in a git repo.
func (s *Server) gitTrackedFiles() []string {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = s.rootDir
	out, err := cmd.Output()
	if err != nil {
		// Not a git repo, fall back to directory listing
		return s.walkFiles()
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

func (s *Server) walkFiles() []string {
	var files []string
	filepath.WalkDir(s.rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(s.rootDir, path)
			files = append(files, rel)
		}
		return nil
	})
	return files
}

// gitStatus returns a map of file path -> status code.
func (s *Server) gitStatus() map[string]string {
	cmd := exec.Command("git", "status", "--porcelain=v1")
	cmd.Dir = s.rootDir
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	result := make(map[string]string)
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		xy := strings.TrimSpace(line[:2])
		file := strings.TrimSpace(line[3:])
		// Handle renames: "R  old -> new"
		if idx := strings.Index(file, " -> "); idx >= 0 {
			file = file[idx+4:]
		}
		switch {
		case strings.Contains(xy, "?"):
			result[file] = "?"
		case strings.Contains(xy, "A"):
			result[file] = "A"
		case strings.Contains(xy, "D"):
			result[file] = "D"
		default:
			result[file] = "M"
		}
	}
	return result
}

// buildTreeFromPaths creates a nested tree from flat file paths.
func buildTreeFromPaths(files []string, statusMap map[string]string) []TreeEntry {
	type node struct {
		name     string
		path     string
		isDir    bool
		status   string
		children map[string]*node
		order    []string // preserve insertion order
	}

	// Mark root as a dir so propagate recurses into its children.
	root := &node{isDir: true, children: make(map[string]*node)}

	for _, file := range files {
		parts := strings.Split(file, "/")
		cur := root
		for i, part := range parts {
			if _, ok := cur.children[part]; !ok {
				isDir := i < len(parts)-1
				p := strings.Join(parts[:i+1], "/")
				n := &node{
					name:     part,
					path:     p,
					isDir:    isDir,
					children: make(map[string]*node),
				}
				if !isDir {
					if st, ok := statusMap[file]; ok {
						n.status = st
					}
				}
				cur.children[part] = n
				cur.order = append(cur.order, part)
			} else if i < len(parts)-1 {
				// Ensure intermediate dirs are marked as dirs
				cur.children[part].isDir = true
			}
			cur = cur.children[part]
		}
	}

	// Propagate status up: if any child is modified, parent dir gets "M"
	var propagate func(n *node) string
	propagate = func(n *node) string {
		if !n.isDir {
			return n.status
		}
		for _, name := range n.order {
			child := n.children[name]
			childStatus := propagate(child)
			if childStatus != "" && n.status == "" {
				n.status = childStatus
			}
		}
		return n.status
	}
	propagate(root)

	var convert func(n *node) []TreeEntry
	convert = func(n *node) []TreeEntry {
		var entries []TreeEntry
		for _, name := range n.order {
			child := n.children[name]
			e := TreeEntry{
				Name:   child.name,
				Path:   child.path,
				IsDir:  child.isDir,
				Status: child.status,
			}
			if child.isDir {
				e.Children = convert(child)
			}
			entries = append(entries, e)
		}
		// Sort: directories first, then by name
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir != entries[j].IsDir {
				return entries[i].IsDir
			}
			return entries[i].Name < entries[j].Name
		})
		return entries
	}

	return convert(root)
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	absPath, err := s.resolveFile(file)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"modTime": info.ModTime().Unix(),
	})
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	absPath, err := s.resolveFile(file)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	content, err := readDocument(absPath)
	if err != nil {
		writeDocumentError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("ETag", documentETag(content))
	_, _ = w.Write(content)
}

func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	absPath, err := s.resolveFile(file)
	if err != nil || !isMarkdown(file) {
		http.Error(w, "Invalid Markdown file", http.StatusBadRequest)
		return
	}
	input, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDocumentSize+1))
	if err != nil || len(input) > maxDocumentSize || isBinary(input) {
		http.Error(w, "Invalid document", http.StatusRequestEntityTooLarge)
		return
	}
	rendered, err := s.renderInput(input, absPath)
	if err != nil {
		http.Error(w, "Render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(rendered)
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	absPath, err := s.resolveFile(file)
	if err != nil || !isMarkdown(file) {
		http.Error(w, "Invalid Markdown file", http.StatusBadRequest)
		return
	}
	current, err := readDocument(absPath)
	if err != nil {
		writeDocumentError(w, err)
		return
	}
	if r.Header.Get("If-Match") == "" {
		http.Error(w, "Missing document revision", http.StatusPreconditionRequired)
		return
	}
	if r.Header.Get("If-Match") != documentETag(current) {
		http.Error(w, "Document changed on disk", http.StatusConflict)
		return
	}
	input, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDocumentSize+1))
	if err != nil || len(input) > maxDocumentSize || isBinary(input) {
		http.Error(w, "Invalid document", http.StatusRequestEntityTooLarge)
		return
	}
	if err := writeDocumentAtomically(absPath, input); err != nil {
		http.Error(w, "Save error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("ETag", documentETag(input))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLocalFile(w http.ResponseWriter, r *http.Request) {
	file := strings.TrimPrefix(r.URL.Path, "/files/")
	absPath, err := s.resolveFile(file)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	http.ServeFile(w, r, absPath)
}

func isMarkdown(file string) bool { return strings.EqualFold(filepath.Ext(file), ".md") }

func documentETag(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("\"%x\"", sum[:])
}

func readDocument(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, os.ErrNotExist
	}
	if info.Size() > maxDocumentSize {
		return nil, errDocumentTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isBinary(data) {
		return nil, errBinaryDocument
	}
	return data, nil
}

func writeDocumentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errDocumentTooLarge):
		http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
	case errors.Is(err, errBinaryDocument):
		http.Error(w, "Binary file", http.StatusUnsupportedMediaType)
	case errors.Is(err, os.ErrNotExist):
		http.Error(w, "Not found", http.StatusNotFound)
	default:
		http.Error(w, "Read error", http.StatusInternalServerError)
	}
}

func writeDocumentAtomically(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mdpreview-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func isBinary(data []byte) bool {
	// Check first 512 bytes for null bytes
	check := data
	if len(check) > 512 {
		check = check[:512]
	}
	for _, b := range check {
		if b == 0 {
			return true
		}
	}
	return false
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		file = s.defaultFile
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	indexBuf := new(bytes.Buffer)
	err := s.indexTemplate.Execute(indexBuf, map[string]any{
		"path":     file,
		"repoName": filepath.Base(s.rootDir),
	})
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Write(indexBuf.Bytes())
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	ws, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		if _, ok := err.(websocket.HandshakeError); !ok {
			s.log.WithError(err).Warn("websocket upgrade failed")
		}
		return
	}

	file := r.URL.Query().Get("file")
	absPath, err := s.resolveFile(file)
	if err != nil {
		s.log.WithError(err).Error("invalid file path")
		ws.Close()
		return
	}

	go s.writer(ws, absPath)
	s.reader(ws)
}

// relURLRe matches src= and href= attributes with any URL.
var relURLRe = regexp.MustCompile(`((?:src|href)\s*=\s*")([^"]+)(")`)

func (s *Server) render(absPath string) ([]byte, error) {
	input, err := readDocument(absPath)
	if err != nil {
		return nil, err
	}
	return s.renderInput(input, absPath)
}

func (s *Server) renderInput(input []byte, absPath string) ([]byte, error) {
	var buf bytes.Buffer
	if err := s.md.Convert(input, &buf); err != nil {
		return nil, err
	}

	// Directory containing this file, relative to rootDir
	fileDir, _ := filepath.Rel(s.rootDir, filepath.Dir(absPath))

	// Rewrite relative URLs
	html := relURLRe.ReplaceAllFunc(buf.Bytes(), func(match []byte) []byte {
		parts := relURLRe.FindSubmatch(match)
		href := string(parts[2])

		// Skip absolute URLs (http://, https://, //, mailto:, etc.)
		if strings.Contains(href, "://") || strings.HasPrefix(href, "//") || strings.HasPrefix(href, "mailto:") {
			return match
		}
		// Skip anchors
		if strings.HasPrefix(href, "#") {
			return match
		}
		// Skip our own static assets
		if strings.HasPrefix(href, "/static/") || strings.HasPrefix(href, "static/") {
			return match
		}

		// Determine if this is src= or href=
		isSrc := strings.HasPrefix(string(parts[1]), "src")

		// For .md links in href, make them navigate within the preview
		if !isSrc && strings.HasSuffix(href, ".md") {
			relPath := filepath.Join(fileDir, href)
			return []byte(string(parts[1]) + "/?file=" + relPath + string(parts[3]))
		}

		// For images and other files, serve from /files/
		relPath := filepath.Join(fileDir, href)
		return []byte(string(parts[1]) + "/files/" + relPath + string(parts[3]))
	})

	// Annotate top-level HTML elements with source line numbers.
	// Parse source markdown to find block-start lines (after blank lines).
	html = annotateSourceLines(input, html)

	return html, nil
}

// topLevelTagRe matches opening tags at the start of a line (top-level HTML elements).
var topLevelTagRe = regexp.MustCompile(`(?m)^<([a-z][a-z0-9]*)`)

func annotateSourceLines(source, html []byte) []byte {
	// Find the start line of each markdown block (blank-line separated).
	lines := bytes.Split(source, []byte("\n"))
	var blockStarts []int
	inBlock := false
	for i, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			inBlock = false
		} else if !inBlock {
			blockStarts = append(blockStarts, i+1) // 1-indexed
			inBlock = true
		}
	}

	// Inject data-source-line="N" into top-level opening tags, one per block.
	bi := 0
	result := topLevelTagRe.ReplaceAllFunc(html, func(match []byte) []byte {
		if bi >= len(blockStarts) {
			return match
		}
		line := blockStarts[bi]
		bi++
		// e.g. <h2 -> <h2 data-source-line="5"
		return []byte(fmt.Sprintf(`%s data-source-line="%d"`, match, line))
	})
	return result
}

func (s *Server) watcher(changes chan<- struct{}, absPath string, done <-chan struct{}) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		s.log.WithError(err).Error("failed to create file watcher")
		return
	}
	defer w.Close()

	// Watch the parent directory to handle vim-style atomic saves (delete+rename).
	dir := filepath.Dir(absPath)
	if err := w.Add(dir); err != nil {
		s.log.WithError(err).Error("failed to watch directory")
		return
	}

	changes <- struct{}{} // Send initial render trigger

	base := filepath.Base(absPath)
	for {
		select {
		case <-s.ctx.Done():
			s.log.Debug("watcher shutting down")
			return
		case <-done:
			return
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			// Only react to events for our target file
			if filepath.Base(event.Name) != base {
				continue
			}
			s.log.WithFields(logrus.Fields{
				"file":  event.Name,
				"event": event.Op,
			}).Debug("file event")

			select {
			case changes <- struct{}{}:
			default:
				// already pending
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			s.log.WithError(err).Warn("file watcher error")
		}
	}
}

func (s *Server) writer(ws *websocket.Conn, absPath string) {
	defer ws.Close()

	done := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(done) }) }
	defer closeDone()

	// Pings keep intermediate proxies from closing idle connections. Browsers
	// auto-pong, and the server's read deadline is 60s, so 30s is ample.
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()

	changes := make(chan struct{}, 1)
	go s.watcher(changes, absPath, done)

	// Debounce: wait for writes to settle before rendering
	const debounce = 150 * time.Millisecond
	var debounceTimer *time.Timer
	renderCh := make(chan struct{}, 1)

	for {
		select {
		case <-s.ctx.Done():
			s.log.Debug("writer shutting down")
			return
		case <-changes:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounce, func() {
				select {
				case renderCh <- struct{}{}:
				default:
				}
			})
		case <-renderCh:
			rendered, err := s.render(absPath)
			if err != nil {
				s.log.WithError(err).Error("failed to render markdown")
				continue
			}
			s.log.Debug("sending rendered content")
			if err := ws.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return
			}
			if err := ws.WriteMessage(websocket.TextMessage, rendered); err != nil {
				s.log.WithError(err).Debug("failed to write message")
				return
			}
		case <-pingTicker.C:
			s.log.Debug("sending ping")
			if err := ws.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
				return
			}
			if err := ws.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				s.log.WithError(err).Debug("failed to send ping")
				return
			}
		}
	}
}

func (s *Server) reader(ws *websocket.Conn) {
	defer ws.Close()

	ws.SetReadLimit(512)

	if err := ws.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		s.log.WithError(err).Error("failed to set read deadline")
		return
	}

	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			if _, _, err := ws.ReadMessage(); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					s.log.WithError(err).Warn("unexpected websocket close")
				}
				return
			}
		}
	}
}

// DiffInfo contains line-level git diff information for a file.
type DiffInfo struct {
	Additions    int   `json:"additions"`
	Deletions    int   `json:"deletions"`
	AddedLines   []int `json:"addedLines,omitempty"`
	ChangedLines []int `json:"changedLines,omitempty"`
	DeletedAt    []int `json:"deletedAt,omitempty"` // line numbers where deletions occurred
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		file = s.defaultFile
	}

	_, err := s.resolveFile(file)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	info := s.gitDiffInfo(file)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func (s *Server) gitDiffInfo(file string) DiffInfo {
	var info DiffInfo

	// Get unified diff with line numbers
	cmd := exec.Command("git", "diff", "--unified=0", "--", file)
	cmd.Dir = s.rootDir
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		// Try staged changes
		cmd = exec.Command("git", "diff", "--unified=0", "--cached", "--", file)
		cmd.Dir = s.rootDir
		out, err = cmd.Output()
		if err != nil || len(out) == 0 {
			return info
		}
	}

	// Parse unified diff hunks: @@ -oldStart,oldCount +newStart,newCount @@
	hunkRe := regexp.MustCompile(`@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
	lines := strings.Split(string(out), "\n")

	for i, line := range lines {
		m := hunkRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		newStart := 0
		newCount := 1
		fmt.Sscanf(m[3], "%d", &newStart)
		if m[4] != "" {
			fmt.Sscanf(m[4], "%d", &newCount)
		}

		// Count additions and deletions in this hunk
		hunkDel := 0
		hunkAdd := 0
		for j := i + 1; j < len(lines); j++ {
			if len(lines[j]) == 0 {
				continue
			}
			if lines[j][0] == '-' {
				hunkDel++
			} else if lines[j][0] == '+' {
				hunkAdd++
			} else if lines[j][0] == '@' {
				break
			}
		}

		info.Additions += hunkAdd
		info.Deletions += hunkDel

		if hunkDel > 0 && hunkAdd == 0 {
			// Pure deletion: mark the line in the new file where content was removed
			info.DeletedAt = append(info.DeletedAt, newStart)
		}

		for l := newStart; l < newStart+newCount; l++ {
			if hunkDel > 0 && hunkAdd > 0 {
				info.ChangedLines = append(info.ChangedLines, l)
			} else if hunkAdd > 0 {
				info.AddedLines = append(info.AddedLines, l)
			}
		}
	}

	return info
}
