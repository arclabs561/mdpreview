package server

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/shurcooL/github_flavored_markdown"
	"github.com/sirupsen/logrus"
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
	renderLocally bool
}

// New creates a new Server given some markdown path.
func New(ctx context.Context, path string, log *logrus.Logger, renderLocally bool) (*Server, error) {
	indexData, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		return nil, err
	}

	indexTemplate, err := template.New("index").Parse(string(indexData))
	if err != nil {
		return nil, err
	}

	return &Server{
		ctx:           ctx,
		path:          path,
		log:           log,
		indexTemplate: indexTemplate,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// Only allow same-origin connections for security
				origin := r.Header.Get("Origin")
				return origin == "" || origin == "http://"+r.Host
			},
		},
		renderLocally: renderLocally,
	}, nil
}

// Run returns handlers to run the server.
func (s *Server) Run() (http.Handler, error) {
	return s.setupHandlers(), nil
}

func (s *Server) setupHandlers() http.Handler {
	staticFileHandler := http.FileServer(http.FS(staticFiles))

	r := mux.NewRouter()
	r.HandleFunc("/", s.handleIndex).Methods("GET")
	r.HandleFunc("/ws", s.handleWebSocket).Methods("GET")
	r.HandleFunc("/content", s.handleGetContent).Methods("GET")
	r.PathPrefix("/").Handler(staticFileHandler).Methods("GET")

	return r
}

func (s *Server) handleGetContent(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile(s.path)
	if err != nil {
		s.log.WithError(err).Error("failed to read file")
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write(content)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	indexBuf := new(bytes.Buffer)
	err := s.indexTemplate.Execute(indexBuf, map[string]interface{}{
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

func (s *Server) render() ([]byte, error) {
	input, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}

	if s.renderLocally {
		return github_flavored_markdown.Markdown(input), nil
	}

	// Use GitHub API for rendering
	req, err := http.NewRequestWithContext(s.ctx, "POST", "https://api.github.com/markdown/raw", bytes.NewReader(input))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
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
				// File was removed or renamed - try to re-add it after a delay
				// This handles editor save patterns (write to temp, rename)
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

	changes := make(chan struct{}, 1) // Buffered to prevent blocking watcher
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
	
	ws.SetReadLimit(5 * 1024 * 1024) // 5MB limit for file content
	
	if err := ws.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
		s.log.WithError(err).Error("failed to set read deadline")
		return
	}
	
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(60 * time.Second))
	})
	
	// Send initial content
	content, err := os.ReadFile(s.path)
	if err == nil {
		msg := map[string]string{
			"type":    "content",
			"content": string(content),
		}
		if data, err := json.Marshal(msg); err == nil {
			if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
				s.log.WithError(err).Error("failed to send initial content")
			}
		}
	}
	
	for {
		select {
		case <-s.ctx.Done():
			s.log.Debug("reader shutting down")
			return
		default:
			_, message, err := ws.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					s.log.WithError(err).Warn("unexpected websocket close")
				}
				return
			}
			
			// Parse message as JSON
			var msg map[string]string
			if err := json.Unmarshal(message, &msg); err != nil {
				s.log.WithError(err).Debug("failed to parse message")
				continue
			}
			
			// Handle different message types
			switch msg["type"] {
			case "save":
				if err := s.saveContent(msg["content"]); err != nil {
					s.log.WithError(err).Error("failed to save file")
					// Send error back to client
					response := map[string]string{
						"type":  "error",
						"error": "Failed to save file",
					}
					if data, err := json.Marshal(response); err == nil {
						ws.WriteMessage(websocket.TextMessage, data)
					}
				} else {
					s.log.Info("file saved successfully")
				}
			}
		}
	}
}

func (s *Server) saveContent(content string) error {
	// Write to a temporary file first, then rename (atomic operation)
	tmpFile := s.path + ".tmp"
	
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		return err
	}
	
	return os.Rename(tmpFile, s.path)
}
