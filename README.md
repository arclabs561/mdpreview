# mdpreview

Markdown preview server with live reload.

## Usage

```sh
go install github.com/henrywallace/mdpreview@latest
mdpreview README.md
```

Opens a local server at http://127.0.0.1:8080 with a GitHub-styled
preview. The page updates automatically when the file changes on disk.

```sh
mdpreview -addr :3000 README.md   # custom port
mdpreview -debug README.md        # verbose logging
```

## Rendering

- GFM via [goldmark](https://github.com/yuin/goldmark) (tables, task lists, strikethrough, autolinks)
- Syntax highlighting via [chroma](https://github.com/alecthomas/chroma) (server-side, no client JS)
- Math via [KaTeX](https://katex.org/) (`$inline$` and `$$block$$`)
- [GitHub Primer](https://primer.style/) CSS with system dark mode detection
