// Package picks loads goph.re's own editorial articles ("Picks": Go software we
// love, totally biased) from Markdown files in picks/ and renders them to HTML.
package picks

import (
	"bufio"
	"bytes"
	"fmt"
	"gophre/env"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

// Dir holds one Markdown file per article; the file name (minus .md) is the slug.
// Files in Dir/drafts/ are always drafts; publishing a pick means moving it up.
var Dir = env.PATH + "picks/"

// Article is one rendered pick.
type Article struct {
	Slug        string
	Title       string
	Subtitle    string
	Date        time.Time
	Updated     time.Time
	Author      string
	Repo        string // e.g. github.com/owner/name
	Homepage    string
	License     string
	Version     string
	Stars       string
	Tags        []string
	Image       string // OpenGraph card, relative to the site root
	Verdict     string // one-line verdict shown on cards
	Draft       bool
	ReadMinutes int
	HTML        template.HTML
}

// URL is the canonical path of the article.
func (a Article) URL() string { return "/picks/" + a.Slug }

var (
	mu       sync.Mutex
	cache    []Article
	cacheKey string

	md = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Typographer,
			extension.Footnote,
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
			),
		),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		// Articles are written by us and may embed small HTML blocks (verdict boxes).
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
	)
)

// All returns published articles, newest first. Drafts are included when withDrafts is true.
func All(withDrafts bool) []Article {
	articles, err := load()
	if err != nil {
		log.Println(err)
		return nil
	}
	out := make([]Article, 0, len(articles))
	for _, a := range articles {
		if a.Draft && !withDrafts {
			continue
		}
		out = append(out, a)
	}
	return out
}

// Get returns one article by slug.
func Get(slug string, withDrafts bool) (Article, bool) {
	for _, a := range All(withDrafts) {
		if a.Slug == slug {
			return a, true
		}
	}
	return Article{}, false
}

// load parses every article, cached until a file in Dir changes.
func load() ([]Article, error) {
	files, err := filepath.Glob(Dir + "*.md")
	if err != nil {
		return nil, fmt.Errorf("Error listing picks: %v.", err)
	}
	// picks/drafts/ is gitignored: work in progress never reaches the public repo.
	drafts, _ := filepath.Glob(Dir + "drafts/*.md")
	files = append(files, drafts...)
	var key strings.Builder
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			fmt.Fprintf(&key, "%s:%d:%d;", f, info.ModTime().UnixNano(), info.Size())
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if cache != nil && cacheKey == key.String() {
		return cache, nil
	}

	articles := make([]Article, 0, len(files))
	for _, f := range files {
		a, err := parseFile(f)
		if err != nil {
			log.Printf("Error parsing pick %s: %v\n", filepath.Base(f), err)
			continue
		}
		if filepath.Base(filepath.Dir(f)) == "drafts" {
			a.Draft = true
		}
		articles = append(articles, a)
	}
	sort.Slice(articles, func(i, j int) bool { return articles[i].Date.After(articles[j].Date) })
	cache, cacheKey = articles, key.String()
	return articles, nil
}

// parseFile reads "---"-delimited "key: value" front matter followed by Markdown.
func parseFile(path string) (Article, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Article{}, err
	}
	a := Article{Slug: strings.TrimSuffix(filepath.Base(path), ".md")}

	body := raw
	if bytes.HasPrefix(raw, []byte("---\n")) {
		end := bytes.Index(raw[4:], []byte("\n---\n"))
		if end < 0 {
			return a, fmt.Errorf("unterminated front matter")
		}
		front := raw[4 : 4+end]
		body = raw[4+end+5:]
		scanner := bufio.NewScanner(bytes.NewReader(front))
		for scanner.Scan() {
			k, v, ok := strings.Cut(scanner.Text(), ":")
			if !ok {
				continue
			}
			v = strings.Trim(strings.TrimSpace(v), `"`)
			switch strings.TrimSpace(k) {
			case "title":
				a.Title = v
			case "subtitle":
				a.Subtitle = v
			case "date":
				a.Date = parseDate(v)
			case "updated":
				a.Updated = parseDate(v)
			case "author":
				a.Author = v
			case "repo":
				a.Repo = v
			case "homepage":
				a.Homepage = v
			case "license":
				a.License = v
			case "version":
				a.Version = v
			case "stars":
				a.Stars = v
			case "image":
				a.Image = v
			case "verdict":
				a.Verdict = v
			case "draft":
				a.Draft = v == "true"
			case "tags":
				for _, t := range strings.Split(v, ",") {
					if t = strings.TrimSpace(t); t != "" {
						a.Tags = append(a.Tags, t)
					}
				}
			}
		}
	}
	if a.Title == "" {
		return a, fmt.Errorf("missing title")
	}
	if a.Date.IsZero() {
		return a, fmt.Errorf("missing or invalid date")
	}

	var out bytes.Buffer
	if err := md.Convert(body, &out); err != nil {
		return a, err
	}
	a.HTML = template.HTML(out.String())
	a.ReadMinutes = max(1, len(strings.Fields(string(body)))/220)
	return a, nil
}

// parseDate accepts "2006-01-02 15:04" (Paris time) or a bare "2006-01-02".
func parseDate(v string) time.Time {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		loc = time.UTC
	}
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, v, loc); err == nil {
			return t
		}
	}
	return time.Time{}
}
