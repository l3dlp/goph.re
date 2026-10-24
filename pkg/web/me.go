package web

import (
	"encoding/json"
	"fmt"
	"gophre/cmd/data"
	"gophre/env"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// MeView is everything the terminal-style /me page renders for one user.
type MeView struct {
	Bookmarks   []MeEntry
	Fresh       []data.Article
	Sources     []MeCount
	MaxSource   int
	Good        int
	Bad         int
	Since       time.Time
	Streak      int
	HeatRows    [][]MeDay
	Kernel      string
	Feeds       int
	HeatMonths  []string
	HeatLabels  []string
	Archive     int
	LastArticle time.Time
}

// MeEntry is a bookmarked article and the moment it was saved.
type MeEntry struct {
	data.Article
	SavedAt time.Time
}

// MeCount is a bookmark count for one source.
type MeCount struct {
	Name  string
	Path  string
	Count int
}

// MeDay is one cell of the vote heatmap.
type MeDay struct {
	Date  time.Time
	Count int
	Level int
}

const (
	meHeatWeeks  = 26
	meFreshSize  = 12
	meTopSources = 8
)

var (
	meMu       sync.Mutex
	meArticles []data.Article
	meIndex    map[string]int
	meCacheKey string
)

// UserBookmarks renders the /me page: the logged-in user's terminal.
func UserBookmarks(c *gin.Context) {
	user, isAuthenticated := GetCurrentUser(c)
	if !isAuthenticated {
		c.Redirect(http.StatusFound, "/auth/github")
		return
	}

	view, err := buildMeView(user.ID, time.Now())
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("Error retrieving bookmarks: %v", err))
		return
	}

	c.HTML(http.StatusOK, "me.html", gin.H{
		"user": user,
		"me":   view,
	})
}

// loadMeArticles returns all articles and an ID index, cached until the posts file changes.
func loadMeArticles() ([]data.Article, map[string]int, error) {
	info, err := os.Stat(env.POSTS)
	if err != nil {
		return nil, nil, fmt.Errorf("Error reading posts file: %v.", err)
	}
	key := info.ModTime().String()

	meMu.Lock()
	defer meMu.Unlock()
	if meArticles != nil && meCacheKey == key {
		return meArticles, meIndex, nil
	}

	file, err := os.ReadFile(env.POSTS)
	if err != nil {
		return nil, nil, fmt.Errorf("Error reading posts file: %v.", err)
	}
	var articles []data.Article
	if err := json.Unmarshal(file, &articles); err != nil {
		return nil, nil, fmt.Errorf("Error decoding posts: %v.", err)
	}
	index := make(map[string]int, len(articles))
	for i, article := range articles {
		index[article.ID] = i
	}
	meArticles, meIndex, meCacheKey = articles, index, key
	return articles, index, nil
}

// buildMeView joins the user's votes with the articles to build every section of /me.
func buildMeView(userID string, now time.Time) (*MeView, error) {
	articles, index, err := loadMeArticles()
	if err != nil {
		return nil, err
	}

	var votes []data.Vote
	file, err := os.ReadFile(env.VOTES)
	if err != nil {
		return nil, fmt.Errorf("Error reading votes file: %v.", err)
	}
	if err := json.Unmarshal(file, &votes); err != nil {
		return nil, fmt.Errorf("Error decoding votes: %v.", err)
	}

	view := &MeView{Archive: len(articles), Kernel: runtime.Version()}
	if file, err := os.ReadFile(env.FEEDS); err == nil {
		var feeds []data.Rss
		if json.Unmarshal(file, &feeds) == nil {
			view.Feeds = len(feeds)
		}
	}
	if len(articles) > 0 {
		view.LastArticle = articles[0].Date
	}

	voted := map[string]bool{}
	perDay := map[string]int{}
	sourceCounts := map[string]*MeCount{}
	for _, vote := range votes {
		if vote.UserID != userID {
			continue
		}
		voted[vote.ArticleID] = true
		if view.Since.IsZero() || vote.Timestamp.Before(view.Since) {
			view.Since = vote.Timestamp
		}
		perDay[vote.Timestamp.In(now.Location()).Format("2006-01-02")]++
		if vote.Value < 0 {
			view.Bad++
			continue
		}
		view.Good++
		i, ok := index[vote.ArticleID]
		if !ok {
			continue
		}
		article := articles[i]
		view.Bookmarks = append(view.Bookmarks, MeEntry{Article: article, SavedAt: vote.Timestamp})
		count, ok := sourceCounts[article.Source.Name]
		if !ok {
			count = &MeCount{Name: article.Source.Name, Path: article.Source.Path}
			sourceCounts[article.Source.Name] = count
		}
		count.Count++
	}

	sort.SliceStable(view.Bookmarks, func(i, j int) bool {
		return view.Bookmarks[i].SavedAt.After(view.Bookmarks[j].SavedAt)
	})

	for _, count := range sourceCounts {
		view.Sources = append(view.Sources, *count)
	}
	sort.Slice(view.Sources, func(i, j int) bool {
		if view.Sources[i].Count != view.Sources[j].Count {
			return view.Sources[i].Count > view.Sources[j].Count
		}
		return view.Sources[i].Name < view.Sources[j].Name
	})
	if len(view.Sources) > meTopSources {
		view.Sources = view.Sources[:meTopSources]
	}
	if len(view.Sources) > 0 {
		view.MaxSource = view.Sources[0].Count
	}

	// Fresh: the newest unvoted articles from the sources the user keeps the most,
	// falling back to everything outside the forums for a new user.
	favorite := map[string]bool{}
	for i, source := range view.Sources {
		if i < 5 {
			favorite[source.Name] = true
		}
	}
	horizon := now.AddDate(0, 0, -14)
	perSource := map[string]int{}
	for _, article := range articles {
		if len(view.Fresh) >= meFreshSize || article.Date.Before(horizon) {
			break
		}
		if voted[article.ID] || article.Date.After(now.Add(24*time.Hour)) {
			continue
		}
		if perSource[article.Source.Name] >= 4 {
			continue
		}
		if favorite[article.Source.Name] || (len(favorite) == 0 && article.Source.Path != "forums") {
			perSource[article.Source.Name]++
			view.Fresh = append(view.Fresh, article)
		}
	}

	// Heatmap: 7 rows (Monday first) by meHeatWeeks columns, oldest week on the left.
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	monday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	start := monday.AddDate(0, 0, -7*(meHeatWeeks-1))
	lastMonth := ""
	view.HeatRows = make([][]MeDay, 7)
	view.HeatLabels = []string{"Mo", "", "We", "", "Fr", "", "Su"}
	for w := 0; w < meHeatWeeks; w++ {
		week := make([]MeDay, 7)
		for d := 0; d < 7; d++ {
			day := start.AddDate(0, 0, w*7+d)
			count := perDay[day.Format("2006-01-02")]
			level := 0
			switch {
			case day.After(today):
				level = -1
			case count >= 10:
				level = 4
			case count >= 5:
				level = 3
			case count >= 2:
				level = 2
			case count == 1:
				level = 1
			}
			week[d] = MeDay{Date: day, Count: count, Level: level}
			view.HeatRows[d] = append(view.HeatRows[d], week[d])
		}
		// Label a column with the month that starts during its week.
		month := ""
		if m := week[6].Date.Format("Jan"); m != lastMonth && week[6].Date.Day() <= 7 {
			month, lastMonth = m, m
		}
		view.HeatMonths = append(view.HeatMonths, month)
	}

	// Streak: consecutive days with at least one vote, ending today or yesterday.
	for day := today; perDay[day.Format("2006-01-02")] > 0 || day.Equal(today); day = day.AddDate(0, 0, -1) {
		if perDay[day.Format("2006-01-02")] > 0 {
			view.Streak++
		}
	}

	return view, nil
}

// meFuncs are template helpers used by me.html.
var meFuncs = map[string]any{
	"bar": func(n, max, width int) string {
		if max == 0 {
			return ""
		}
		filled := n * width / max
		if n > 0 && filled == 0 {
			filled = 1
		}
		bar := make([]rune, width)
		for i := range bar {
			if i < filled {
				bar[i] = '|'
			} else {
				bar[i] = ' '
			}
		}
		return string(bar)
	},
	"repeat": strings.Repeat,
	"days": func(t time.Time) int {
		if t.IsZero() {
			return 0
		}
		return int(time.Since(t).Hours() / 24)
	},
}
