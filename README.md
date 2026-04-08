# mdpreview

Markdown preview server with live reload.

## Usage

```sh
go install github.com/arclabs561/mdpreview@v0.1.0
mdpreview README.md
```

Or without installing:

```sh
go run github.com/arclabs561/mdpreview@v0.1.0 README.md
```

Opens a local server at http://127.0.0.1:8080 with a GitHub-styled
preview. The page updates automatically when the file changes on disk.

Pass a directory to browse all files with a sidebar tree:

```sh
mdpreview .                          # serve current directory
mdpreview -addr :3000 README.md      # custom port
mdpreview -debug README.md           # verbose logging
```

## Rendering

- GFM via [goldmark](https://github.com/yuin/goldmark) (tables, task lists, strikethrough, autolinks)
- Syntax highlighting via [Shiki](https://shiki.style/) with `github-light` / `github-dark` themes (TextMate grammars, same as VS Code and GitHub)
- Math via [KaTeX](https://katex.org/) (`$inline$` and `$$block$$`)
- GitHub alerts (`> [!NOTE]`, `> [!WARNING]`, `> [!TIP]`, `> [!IMPORTANT]`, `> [!CAUTION]`)
- Colors from [Primer](https://primer.style/) design tokens with system dark mode detection
- Relative images and links resolve from the markdown file's directory
- Live diff highlights on edit (fading yellow for changes, green for additions)
- Git diff gutter and `+N` / `-N` stats for uncommitted changes

## Screenshot

Capture rendered pages as images (requires Chrome/Chromium):

```sh
mdpreview screenshot README.md                # single file -> README.png
mdpreview screenshot -o shot.png README.md    # explicit output
mdpreview screenshot -dark README.md          # force dark mode
mdpreview screenshot -light README.md         # force light mode
mdpreview screenshot .                        # directory -> one PNG per .md
mdpreview screenshot -concat .                # directory -> one tall image
```

## Development

Rebuild the client bundle after editing `client/main.ts`:

```sh
make client   # requires bun or npm
make build    # or just: make
```
