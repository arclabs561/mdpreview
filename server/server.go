package server

import (
	"bytes"
	"context"
	"embed"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

//go:embed static/*
var staticFiles embed.FS

// Server serves a HTML rendered Markdown preview. It can serve a single file
// or a directory (with file selection via ?file= query parameter).
type Server struct {
	ctx           context.Context
	rootDir       string // directory containing the markdown file(s)
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
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(html.WithUnsafe()),
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

	return &Server{
		ctx:           ctx,
		rootDir:       rootDir,
		defaultFile:   defaultFile,
		log:           log,
		indexTemplate: indexTemplate,
		md:            md,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				return origin == "" || origin == "http://"+r.Host
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

// resolveFile returns the absolute path for a file query, ensuring it's
// within rootDir (path traversal protection).
func (s *Server) resolveFile(file string) (string, error) {
	if file == "" {
		file = s.defaultFile
	}
	// Clean and resolve
	clean := filepath.Clean(file)
	abs := filepath.Join(s.rootDir, clean)
	// Ensure it's within rootDir
	if !strings.HasPrefix(abs, s.rootDir) {
		return "", os.ErrPermission
	}
	return abs, nil
}

// Run returns handlers to run the server.
func (s *Server) Run() (http.Handler, error) {
	return s.setupHandlers(), nil
}

func (s *Server) setupHandlers() http.Handler {
	staticFileHandler := http.FileServer(http.FS(staticFiles))
	localFileHandler := http.StripPrefix("/files/", http.FileServer(http.Dir(s.rootDir)))

	r := mux.NewRouter()
	r.HandleFunc("/", s.handleIndex).Methods("GET")
	r.HandleFunc("/ws", s.handleWebSocket).Methods("GET")
	r.PathPrefix("/files/").Handler(localFileHandler).Methods("GET")
	r.PathPrefix("/").Handler(staticFileHandler).Methods("GET")

	return r
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	file := r.URL.Query().Get("file")
	if file == "" {
		file = s.defaultFile
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	indexBuf := new(bytes.Buffer)
	err := s.indexTemplate.Execute(indexBuf, map[string]any{
		"path": file,
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
			s.log.WithError(err)
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

// rewriteRelativeURLs rewrites relative src= and href= attributes to
// point to the /files/ endpoint so images and links resolve correctly.
var relURLRe = regexp.MustCompile(`((?:src|href)\s*=\s*")([^":/][^"]*)(")`)

func (s *Server) render(absPath string) ([]byte, error) {
	input, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

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
		attr := string(parts[1][:4]) // "src=" or "href"

		// Skip anchors and absolute paths
		if len(href) > 0 && href[0] == '#' {
			return match
		}
		if strings.HasPrefix(href, "static/") {
			return match
		}

		// For .md links in href, make them navigate within the preview
		if attr == "href" && strings.HasSuffix(href, ".md") {
			relPath := filepath.Join(fileDir, href)
			return []byte(string(parts[1]) + "/?file=" + relPath + string(parts[3]))
		}

		// For images and other files, serve from /files/
		relPath := filepath.Join(fileDir, href)
		return []byte(string(parts[1]) + "/files/" + relPath + string(parts[3]))
	})

	return html, nil
}

func (s *Server) watcher(changes chan<- struct{}, absPath string) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		s.log.WithError(err).Error("failed to create file watcher")
		return
	}
	defer w.Close()

	if err := w.Add(absPath); err != nil {
		s.log.WithError(err).Error("failed to watch file")
		return
	}

	changes <- struct{}{} // Send initial render trigger

	for {
		select {
		case <-s.ctx.Done():
			s.log.Debug("watcher shutting down")
			return
		case event, ok := <-w.Events:
			if !ok {
				return
			}
			s.log.WithFields(logrus.Fields{
				"file":  event.Name,
				"event": event.Op,
			}).Debug("file event")

			switch event.Op {
			case fsnotify.Remove, fsnotify.Rename:
				go func() {
					time.Sleep(100 * time.Millisecond)
					if err := w.Add(absPath); err != nil {
						s.log.WithError(err).Debug("failed to re-add watch")
					}
				}()
				changes <- struct{}{}
			case fsnotify.Write, fsnotify.Chmod:
				changes <- struct{}{}
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

	pingInterval := 2 * time.Second
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	changes := make(chan struct{}, 1)
	go s.watcher(changes, absPath)

	for {
		select {
		case <-s.ctx.Done():
			s.log.Debug("writer shutting down")
			return
		case <-changes:
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
