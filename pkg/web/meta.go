package web

import "strings"

// SiteURL is the public origin used in canonical, OpenGraph and manifest URLs.
const SiteURL = "https://goph.re"

// PageMeta feeds the shared "head" template: description, OpenGraph card and canonical URL.
type PageMeta struct {
	Title       string
	Description string
	Image       string
	ImageAlt    string
	Canonical   string
}

var sectionMeta = map[string]PageMeta{
	"golang":    {Title: "Official Go", Description: "The Go blog, release notes and every go1.x.y tag as it ships, on goph.re."},
	"experts":   {Title: "Go experts", Description: "Russ Cox, Filippo Valsorda, Three Dots Labs, Alex Edwards and more Go experts, on goph.re."},
	"blogs":     {Title: "Go blogs", Description: "Independent writing about Go, from tutorials to war stories, on goph.re."},
	"podcasts":  {Title: "Go podcasts", Description: "Cup o' Go, Go Podcast() and the archive of Go Time, on goph.re."},
	"community": {Title: "Go community", Description: "Lobsters, Hacker News and GopherCon talks about Go, on goph.re."},
	"forums":    {Title: "Go forums", Description: "r/golang and the Golang Bridge forum, live on goph.re."},
}

// metaFor returns the head metadata of a page ("feed", "pulse", "me") and optional section path.
func metaFor(page, path string, all bool) PageMeta {
	m := PageMeta{
		Title:       "GOPH.RE — the Go coders news feed",
		Description: "Go releases, experts, blogs, podcasts and forums in one infinite wall, updated every hour.",
		Image:       "feed",
		Canonical:   "/",
	}
	switch {
	case page == "pulse":
		m = PageMeta{Title: "The pulse of Go — goph.re", Description: "What's rising in Go this week, what's worth reading, and which sources are alive.", Image: "pulse", Canonical: "/pulse"}
	case page == "me":
		m = PageMeta{Title: "Your Go terminal — goph.re", Description: "Your goph.re bookmarks in a keyboard-driven terminal: neofetch, ls, tail and vim keys.", Image: "me", Canonical: "/me"}
	case all:
		m = PageMeta{Title: "Everything Go — goph.re", Description: "Every Go article from every source goph.re follows, unfiltered.", Image: "all", Canonical: "/all"}
	case path != "":
		if s, ok := sectionMeta[path]; ok {
			m = PageMeta{Title: s.Title + " — goph.re", Description: s.Description, Image: path, Canonical: "/" + path}
		} else {
			m.Title = "goph.re/" + strings.ToLower(path)
			m.Canonical = "/" + path
		}
	}
	m.Image = SiteURL + "/gfx/og/" + m.Image + ".jpg"
	m.ImageAlt = m.Title + " — illustrated gophers reading Go news"
	return m
}
