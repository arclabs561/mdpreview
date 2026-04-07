# mdpreview

Markdown preview server with live reload.

## Usage

```sh
go install github.com/arclabs561/mdpreview@latest
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
- Syntax highlighting via [Shiki](https://shiki.style/) with `github-light` / `github-dark` themes (TextMate grammars, same as VS Code and GitHub)
- Math via [KaTeX](https://katex.org/) (`$inline$` and `$$block$$`)
- GitHub alerts (`> [!NOTE]`, `> [!WARNING]`, `> [!TIP]`, `> [!IMPORTANT]`, `> [!CAUTION]`)
- [GitHub Primer](https://primer.style/) CSS with system dark mode detection
- Relative images and links resolve from the markdown file's directory

## Development

Rebuild the client bundle after editing `client/main.ts`:

```sh
make client   # requires bun or npm
make build    # or just: make
```
