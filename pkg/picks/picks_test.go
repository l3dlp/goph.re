package picks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFileAndDrafts(t *testing.T) {
	dir := t.TempDir()
	Dir = dir + "/"
	cache = nil
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("live.md", "---\ntitle: Live pick\nsubtitle: A subtitle\ndate: 2026-10-24\ntags: go, rf\nrepo: github.com/x/y\n---\n## Hello\n\n```go\nfunc main() {}\n```\n")
	write("draft.md", "---\ntitle: Draft pick\ndate: 2026-10-25\ndraft: true\n---\nSoon.\n")
	write("broken.md", "---\ndate: 2026-10-26\n---\nNo title.\n")

	if got := All(false); len(got) != 1 || got[0].Slug != "live" {
		t.Fatalf("All(false) = %+v, want only the live pick", got)
	}
	if got := All(true); len(got) != 2 || got[0].Slug != "draft" {
		t.Fatalf("All(true) should include the draft first (newest), got %d articles", len(got))
	}
	a, ok := Get("live", false)
	if !ok {
		t.Fatal("live pick not found")
	}
	if a.Subtitle != "A subtitle" || a.Repo != "github.com/x/y" || strings.Join(a.Tags, "|") != "go|rf" {
		t.Errorf("front matter not parsed: %+v", a)
	}
	html := string(a.HTML)
	if !strings.Contains(html, `<h2 id="hello">Hello</h2>`) || !strings.Contains(html, `class="chroma"`) {
		t.Errorf("unexpected HTML: %s", html)
	}
	if _, ok := Get("draft", false); ok {
		t.Error("draft must not be published")
	}
}
