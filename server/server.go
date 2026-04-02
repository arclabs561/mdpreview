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

// Server serves a HTML rendered Markdown preview of a Markdown file specified
// at path. Whenever the path is written to, the rendering will update
// dynamically.
type Server struct {
	ctx           context.Context
	path          string
	indexTemplate *template.Template
	upgrader      websocket.Upgrader
	log           *logrus.Logger
	md            goldmark.Markdown
}

// New creates a new Server given some markdown path.
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

	return &Server{
		ctx:           ctx,
		path:          path,
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

// Run returns handlers to run the server.
func (s *Server) Run() (http.Handler, error) {
	return s.setupHandlers(), nil
}

func (s *Server) setupHandlers() http.Handler {
	staticFileHandler := http.FileServer(http.FS(staticFiles))

	// Serve files from the markdown file's directory for relative paths
	mdDir := filepath.Dir(s.path)
	localFileHandler := http.StripPrefix("/files/", http.FileServer(http.Dir(mdDir)))

	r := mux.NewRouter()
	r.HandleFunc("/", s.handleIndex).Methods("GET")
	r.HandleFunc("/ws", s.handleWebSocket).Methods("GET")
	r.PathPrefix("/files/").Handler(localFileHandler).Methods("GET")
	r.PathPrefix("/").Handler(staticFileHandler).Methods("GET")

	return r
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	indexBuf := new(bytes.Buffer)
	err := s.indexTemplate.Execute(indexBuf, map[string]any{
		"path": filepath.Base(s.path),
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

	go s.writer(ws)
	s.reader(ws)
}

// rewriteRelativeURLs rewrites relative src= and href= attributes to
// point to the /files/ endpoint so images and links resolve correctly.
var relURLRe = regexp.MustCompile(`((?:src|href)\s*=\s*")([^":/][^"]*)(")`)

func (s *Server) render() ([]byte, error) {
	input, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := s.md.Convert(input, &buf); err != nil {
		return nil, err
	}

	// Rewrite relative URLs to serve from /files/
	html := relURLRe.ReplaceAllFunc(buf.Bytes(), func(match []byte) []byte {
		parts := relURLRe.FindSubmatch(match)
		path := string(parts[2])
		// Skip static/ paths (our own assets) and anchors
		if len(path) > 0 && path[0] == '#' {
			return match
		}
		if len(path) > 7 && path[:7] == "static/" {
			return match
		}
		return append(parts[1], append([]byte("/files/"+path), parts[3]...)...)
	})

	return html, nil
}

func (s *Server) watcher(changes chan<- struct{}) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		s.log.WithError(err).Error("failed to create file watcher")
		return
	}
	defer w.Close()

	err = w.Add(s.path)
	if err != nil {
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
					if err := w.Add(s.path); err != nil {
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

func (s *Server) writer(ws *websocket.Conn) {
	defer ws.Close()

	pingInterval := 2 * time.Second
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	changes := make(chan struct{}, 1)
	go s.watcher(changes)

	for {
		select {
		case <-s.ctx.Done():
			s.log.Debug("writer shutting down")
			return
		case <-changes:
			rendered, err := s.render()
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
