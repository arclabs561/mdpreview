# mdpreview

Markdown preview with live reload.

`glow` renders Markdown in the terminal and `grip` renders through GitHub's
API; mdpreview renders locally in the browser, with math, Mermaid, change
highlighting and a HEAD-vs-working-copy diff.

## Usage

```sh
go install github.com/arclabs561/mdpreview@main
mdpreview README.md
```

Or without installing:

```sh
go run github.com/arclabs561/mdpreview@main README.md
```

Opens a local server at http://127.0.0.1:8080 with GitHub-style rendering
(GFM tables, task lists, footnotes, emoji, math, Mermaid). Raw HTML in the
Markdown is not rendered, so `<details>`, `<img>` and similar tags that GitHub
allows will not appear. The page updates automatically when the file changes
on disk, highlighting what changed.

```sh
mdpreview .                          # serve current directory
mdpreview -addr :3000 README.md      # custom port
mdpreview -no-open README.md         # don't open browser
```

## Editing

The **Edit** button opens the Markdown source beside the preview. Cmd/Ctrl-S
saves to disk; Cmd/Ctrl-B, -I and -E wrap the selection in bold, italic or code.
If the file changed on disk since it was opened, the save is refused and the
editor offers to reload the disk version or keep your draft.

## Diff

View rendered markdown changes side by side (HEAD vs working copy):

```sh
mdpreview diff README.md             # opens browser with side-by-side view
mdpreview diff -o diff.png README.md # save as screenshot
```

The live preview also shows diff indicators: fading highlights on each
edit, a git diff gutter for uncommitted changes, and `+N` / `-N` stats
in the header.

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

## PDF

Export to PDF:

```sh
mdpreview pdf README.md              # -> README.pdf
mdpreview pdf -o out.pdf README.md   # explicit output
```

## Rendering

- GFM via [goldmark](https://github.com/yuin/goldmark) (tables, task lists, strikethrough, autolinks)
- Syntax highlighting via [Shiki](https://shiki.style/) with `github-light` / `github-dark` themes
- Math via [KaTeX](https://katex.org/) (`$inline$` and `$$block$$`)
- GitHub alerts (`> [!NOTE]`, `> [!WARNING]`, `> [!TIP]`, `> [!IMPORTANT]`, `> [!CAUTION]`)
- Mermaid diagrams
- Colors from [Primer](https://primer.style/) design tokens with system dark mode detection

## Development

Rebuild the client bundle after editing `client/main.ts`:

```sh
make client   # requires bun or npm
make build    # or just: make
make e2e      # requires Playwright's Chromium browser
```
