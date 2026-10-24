package web

import (
	"encoding/xml"
	"gophre/pkg/picks"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// adminID is the GitHub user allowed to preview drafts (same as RequireAdmin).
const adminID = "60765"

func picksMeta() PageMeta {
	return PageMeta{
		Title:       "Picks — Go software we love, totally biased · goph.re",
		Description: "Long reads about Go projects we love: we read the code, we explain the clever bits, and we don't pretend to be neutral.",
		Image:       SiteURL + "/gfx/og/picks.jpg",
		ImageAlt:    "goph.re Picks — Go software we love",
		Canonical:   "/picks",
	}
}

// PicksIndex lists the published picks.
func PicksIndex(c *gin.Context) {
	user, _ := GetCurrentUser(c)
	c.HTML(http.StatusOK, "picks.html", gin.H{
		"user":     user,
		"page":     "picks",
		"path":     "",
		"meta":     picksMeta(),
		"articles": picks.All(user.ID == adminID && c.Query("draft") == "1"),
	})
}

// PickArticle renders one pick.
func PickArticle(c *gin.Context) {
	user, _ := GetCurrentUser(c)
	a, ok := picks.Get(c.Param("slug"), user.ID == adminID && c.Query("draft") == "1")
	if !ok {
		c.Redirect(http.StatusFound, "/picks")
		return
	}
	meta := PageMeta{
		Title:       a.Title + " · goph.re picks",
		Description: a.Subtitle,
		Image:       SiteURL + "/gfx/og/picks.jpg",
		ImageAlt:    a.Title,
		Canonical:   a.URL(),
	}
	if a.Image != "" {
		meta.Image = SiteURL + a.Image
	}
	c.HTML(http.StatusOK, "pick.html", gin.H{
		"user":    user,
		"page":    "picks",
		"path":    "",
		"meta":    meta,
		"article": a,
		"more":    otherPicks(a.Slug, 3),
	})
}

func otherPicks(slug string, n int) []picks.Article {
	var out []picks.Article
	for _, a := range picks.All(false) {
		if a.Slug != slug && len(out) < n {
			out = append(out, a)
		}
	}
	return out
}

type atomFeed struct {
	XMLName  xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	Title    string      `xml:"title"`
	Subtitle string      `xml:"subtitle"`
	ID       string      `xml:"id"`
	Updated  string      `xml:"updated"`
	Links    []atomLink  `xml:"link"`
	Icon     string      `xml:"icon"`
	Entries  []atomEntry `xml:"entry"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr,omitempty"`
}

type atomEntry struct {
	Title     string      `xml:"title"`
	ID        string      `xml:"id"`
	Link      atomLink    `xml:"link"`
	Published string      `xml:"published"`
	Updated   string      `xml:"updated"`
	Summary   string      `xml:"summary"`
	Content   atomContent `xml:"content"`
	Author    struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Categories []atomCategory `xml:"category"`
}

type atomContent struct {
	Type string `xml:"type,attr"`
	Body string `xml:",chardata"`
}

type atomCategory struct {
	Term string `xml:"term,attr"`
}

// PicksFeed serves the picks as an Atom feed; goph.re itself follows it as a source.
func PicksFeed(c *gin.Context) {
	articles := picks.All(false)
	feed := atomFeed{
		Title:    "goph.re picks",
		Subtitle: "Go software we love. Totally biased.",
		ID:       SiteURL + "/picks",
		Updated:  time.Now().UTC().Format(time.RFC3339),
		Links:    []atomLink{{Href: SiteURL + "/picks/feed.xml", Rel: "self"}, {Href: SiteURL + "/picks"}},
		Icon:     SiteURL + "/gfx/icons/icon-192.png",
	}
	if len(articles) > 0 {
		feed.Updated = latest(articles[0]).UTC().Format(time.RFC3339)
	}
	for _, a := range articles {
		e := atomEntry{
			Title:     a.Title,
			ID:        SiteURL + a.URL(),
			Link:      atomLink{Href: SiteURL + a.URL()},
			Published: a.Date.UTC().Format(time.RFC3339),
			Updated:   latest(a).UTC().Format(time.RFC3339),
			Summary:   a.Subtitle,
			Content:   atomContent{Type: "html", Body: string(a.HTML)},
		}
		e.Author.Name = a.Author
		for _, t := range a.Tags {
			e.Categories = append(e.Categories, atomCategory{Term: t})
		}
		feed.Entries = append(feed.Entries, e)
	}
	out, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		c.String(http.StatusInternalServerError, "Error encoding feed.")
		return
	}
	c.Header("Cache-Control", "public, max-age=600")
	c.Data(http.StatusOK, "application/atom+xml; charset=utf-8", append([]byte(xml.Header), out...))
}

func latest(a picks.Article) time.Time {
	if a.Updated.After(a.Date) {
		return a.Updated
	}
	return a.Date
}
