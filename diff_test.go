package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDiffHTMLEscapesFilename(t *testing.T) {
	got := buildDiffHTML(`dir/a<b&"c".md`)
	want := `a&lt;b&amp;&#34;c&#34;.md`
	if !strings.Contains(got, "<title>diff: "+want+"</title>") {
		t.Fatalf("title does not contain escaped name %q:\n%s", want, got)
	}
	if strings.Contains(got, `a<b`) {
		t.Fatal("raw '<' from the filename reached the page")
	}
	for _, placeholder := range []string{"OLD_URL", "NEW_URL"} {
		if strings.Count(got, placeholder) != 1 {
			t.Errorf("want exactly one %s placeholder", placeholder)
		}
	}
}

func TestGitRelPathAndShowHead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	sub := filepath.Join(root, "docs")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sub, "README.md")
	if err := os.WriteFile(file, []byte("# committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("init", "-q")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	if err := os.WriteFile(file, []byte("# working copy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Resolve symlinks (macOS /var -> /private/var) the way git reports the root.
	realFile, err := filepath.EvalSymlinks(file)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := gitRelPath(sub, realFile)
	if err != nil {
		t.Fatal(err)
	}
	if rel != filepath.Join("docs", "README.md") {
		t.Fatalf("gitRelPath = %q, want docs/README.md", rel)
	}
	head, err := gitShowHead(sub, rel)
	if err != nil {
		t.Fatal(err)
	}
	if string(head) != "# committed\n" {
		t.Fatalf("gitShowHead = %q, want the committed content", head)
	}
}
