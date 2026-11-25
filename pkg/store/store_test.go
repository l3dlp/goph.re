package store

import (
	"path/filepath"
	"testing"
	"time"

	"gophre/cmd/data"
)

func TestInsertSearchVote(t *testing.T) {
	Path = filepath.Join(t.TempDir(), "test.db")
	t.Cleanup(func() { Close() })

	src := data.Rss{Path: "blogs", Name: "Blog", URL: "https://blog.example/rss"}
	now := time.Now().UTC().Truncate(time.Second)
	articles := []data.Article{
		{ID: "a", URL: "https://x/a", Source: src, Name: "Goroutine leaks explained", Resume: `<p>Find <b>leaks</b></p><script>alert(1)</script>`, Date: now},
		{ID: "b", URL: "https://x/b", Source: src, Name: "Generics in practice", Resume: "Iterators &amp; generics and more", Date: now.Add(-time.Hour)},
		{ID: "c", URL: "https://x/a", Source: src, Name: "Same URL, other title", Date: now},
	}
	inserted, err := Insert(articles)
	if err != nil {
		t.Fatal(err)
	}
	if len(inserted) != 2 {
		t.Fatalf("Insert kept %d articles, want 2 (duplicate URL skipped)", len(inserted))
	}
	if again, _ := Insert(articles[:1]); len(again) != 0 {
		t.Fatalf("re-inserting a known URL returned %d articles", len(again))
	}

	got, ok, err := Get("a")
	if err != nil || !ok {
		t.Fatalf("Get(a) = %v, %v", ok, err)
	}
	if got.Resume != "<p>Find <b>leaks</b></p>" || !got.Date.Equal(now) || got.Source != src {
		t.Fatalf("Get(a) = %+v", got)
	}

	recent, _ := Recent("", 0, 10)
	if len(recent) != 2 || recent[0].ID != "a" {
		t.Fatalf("Recent = %+v", recent)
	}
	if other, _ := Recent("forums", 0, 10); len(other) != 0 {
		t.Fatalf("Recent(forums) = %d articles, want 0", len(other))
	}

	for q, want := range map[string]string{"gorout": "a", "ITERATOR": "b", `"generics" -) AND`: "b", "leaks": "a"} {
		res, err := Search(q, 0, 10)
		if err != nil || len(res) != 1 || res[0].ID != want {
			t.Fatalf("Search(%q) = %+v, %v; want %s", q, res, err, want)
		}
	}
	if res, _ := Search("script alert", 0, 10); len(res) != 0 {
		t.Fatalf("sanitized markup should not be indexed, got %d", len(res))
	}

	if err := Vote("a", "u1", 1, now); err != nil {
		t.Fatal(err)
	}
	Vote("a", "u2", 1, now)
	Vote("a", "u2", -1, now.Add(time.Second)) // a user's new vote replaces the old one
	if a, _, _ := Get("a"); a.Vote != 0 {
		t.Fatalf("vote total = %d, want 0", a.Vote)
	}
	votes, _ := UserVotes("u2")
	if len(votes) != 1 || votes[0].Value != -1 {
		t.Fatalf("UserVotes(u2) = %+v", votes)
	}

	byID, _ := ByID([]string{"a", "b", "missing"})
	if len(byID) != 2 {
		t.Fatalf("ByID found %d, want 2", len(byID))
	}

	if err := Reset(); err != nil {
		t.Fatal(err)
	}
	if s, _ := GetStats(); s.Count != 0 {
		t.Fatalf("after Reset, %d articles remain", s.Count)
	}
	if res, _ := Search("generics", 0, 10); len(res) != 0 {
		t.Fatalf("after Reset, search still finds %d articles", len(res))
	}
}
