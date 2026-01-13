# mdpreview

A beautiful, modern WYSIWYG Markdown editor and preview tool with GitHub-flavored styling, math rendering, and real-time collaboration features.

[![Go Report Card](https://goreportcard.com/badge/github.com/henrywallace/mdpreview)](https://goreportcard.com/report/github.com/henrywallace/mdpreview)
[![GoDoc](https://godoc.org/github.com/henrywallace/mdpreview?status.svg)](https://godoc.org/github.com/henrywallace/mdpreview)

## Features

### ✨ WYSIWYG Editing
- **True WYSIWYG mode** - Edit and see formatted output simultaneously (like Notion or Typora)
- **Markdown mode** - Classic markdown editing with syntax highlighting
- **Seamless switching** - Toggle between WYSIWYG and markdown modes instantly
- **Rich toolbar** - Easy formatting with visual controls

### 🎨 Modern Design
- **GitHub color scheme** - Authentic GitHub Primer design system
- **Dark mode** - Automatic system theme detection + manual toggle
- **Responsive** - Works beautifully on desktop, tablet, and mobile
- **Clean interface** - Distraction-free writing experience

### 🧮 Advanced Features
- **Math rendering** - Full LaTeX support via KaTeX (`$inline$` and `$$block$$`)
- **Syntax highlighting** - Beautiful code blocks with Highlight.js
- **Tables** - Full GFM table support
- **Task lists** - Interactive checkboxes `- [ ] Todo`
- **Auto-linking** - URLs automatically become clickable links

### ⚡ Performance & Reliability
- **Real-time sync** - WebSocket-based instant updates
- **Auto-save** - Saves after 1 second of inactivity
- **Live file watching** - External changes reflected immediately  
- **Atomic writes** - Safe file operations prevent data loss
- **Fast** - Built with Go for maximum performance
- **Graceful shutdown** - Clean connection handling

### 🔒 Security
- **Origin checking** - CSRF protection for WebSocket connections
- **Sandboxed rendering** - Safe HTML rendering
- **Error handling** - Comprehensive error recovery

## Installation

### Using Go install (recommended)

```sh
go install github.com/henrywallace/mdpreview@latest
```

### Building from source

```sh
git clone https://github.com/henrywallace/mdpreview.git
cd mdpreview
make install
```

## Usage

### Basic usage

Start the preview server for any Markdown file:

```sh
mdpreview README.md
```

Then open your browser to [http://127.0.0.1:8080](http://127.0.0.1:8080)

The preview will automatically update whenever you save changes to the file.

### Command-line options

```sh
# Serve on a custom port
mdpreview -addr :3000 README.md

# Serve on all interfaces
mdpreview -addr 0.0.0.0:8080 README.md

# Use GitHub API for rendering (requires internet)
mdpreview -api README.md

# Enable debug logging
mdpreview -debug README.md
```

### Editor modes

The editor supports two modes:
- **WYSIWYG mode** (default) - Edit with live formatting, like a word processor
- **Markdown mode** - Traditional markdown editing with syntax highlighting

Toggle between modes using the button in the toolbar or press `Ctrl+Shift+M`.

### Math equations

Write LaTeX math equations inline or in blocks:

```markdown
Inline math: $E = mc^2$

Block math:
$$
\int_{-\infty}^{\infty} e^{-x^2} dx = \sqrt{\pi}
$$
```

### Keyboard shortcuts

- `Ctrl+S` / `Cmd+S` - Save file
- `Ctrl+Shift+M` / `Cmd+Shift+M` - Toggle WYSIWYG/Markdown mode
- Standard editor shortcuts (Ctrl+B for bold, Ctrl+I for italic, etc.)

### Graceful shutdown

Press `Ctrl+C` to stop the server. The server handles shutdown gracefully, closing all WebSocket connections properly.

## Development

### Requirements

- Go 1.21 or later

### Building

```sh
make build
```

### Running tests

```sh
make test
```

### Linting

```sh
make lint
```

### Updating GitHub CSS

To update the GitHub Markdown CSS styling:

```sh
make css
```

## Screenshots

### WYSIWYG Mode
Edit and see the formatted output in real-time:
```
[Imagine a beautiful WYSIWYG editor with GitHub styling]
```

### Dark Mode
Automatic theme switching based on system preferences:
```
[Imagine the same editor in gorgeous dark mode]
```

## How it works

1. **Server starts** - Go HTTP server with WebSocket support
2. **WYSIWYG editor** - Toast UI Editor with GitHub styling loads in browser
3. **Two-way sync** - Edit in browser → auto-save to file → watch for external changes
4. **Math & code** - KaTeX renders equations, Highlight.js beautifies code
5. **Live collaboration** - Multiple clients can view the same file simultaneously

## Architecture

```
┌─────────────────────┐
│   WYSIWYG Editor    │
│  (Toast UI Editor)  │
│  + KaTeX + Highlight│
└──────────┬──────────┘
           │ WebSocket (bidirectional)
           │
┌──────────▼──────────┐
│   WebSocket Server  │
│   (Gorilla WS)      │
└──────────┬──────────┘
           │
      ┌────┴────┐
      │         │
┌─────▼────┐ ┌─▼────────────┐
│   File   │ │   Markdown   │
│  Watcher │ │   Renderer   │
│(fsnotify)│ │  (GFM/API)   │
└──────────┘ └──────────────┘
      │
┌─────▼──────┐
│  .md File  │
└────────────┘
```

## Technical Stack

### Backend (Go)
- **Server**: Gorilla Mux for routing
- **WebSocket**: Gorilla WebSocket for real-time communication
- **Markdown**: shurcooL/github_flavored_markdown for GFM rendering
- **File watching**: fsnotify for cross-platform file monitoring
- **Logging**: Logrus for structured logging
- **Context**: Full context support for cancellation and timeouts

### Frontend (JavaScript)
- **Editor**: Toast UI Editor v3 (WYSIWYG + Markdown modes)
- **Math**: KaTeX v0.16.9 for LaTeX rendering
- **Syntax highlighting**: Highlight.js v11.9.0
- **Styling**: GitHub Primer CSS v21 + custom dark mode
- **No build step**: All dependencies loaded via CDN

### Features
- **Atomic file writes**: Temp file + rename for data safety
- **Auto-save**: Debounced saves (1s after last edit)
- **Reconnection**: Automatic WebSocket reconnection on disconnect
- **Origin checking**: CSRF protection
- **Graceful shutdown**: Proper cleanup on SIGINT/SIGTERM

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

### Running checks before commit

```sh
./check.sh
```

This will:
- Run `go fmt`
- Run `go vet`
- Run `staticcheck`
- Run all tests
- Build the binary

## License

MIT License - see [LICENSE](LICENSE) file for details

## Acknowledgments

- Uses [github_flavored_markdown](https://github.com/shurcooL/github_flavored_markdown) for rendering
- Inspired by various Markdown preview tools
- GitHub for the beautiful Markdown styling

