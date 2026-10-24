package web

import (
	"encoding/json"
	"fmt"
	"gophre/cmd/data"
	"gophre/env"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
)

// Pulse is the weekly "state of Go news" digest rendered on /pulse.
type Pulse struct {
	UpdatedAt    time.Time
	Week         int
	PrevWeek     int
	SignalWeek   int
	DeltaPct     int
	Total        int
	LiveSources  int
	TotalSources int
	Days         []PulseDay
	MaxDay       int
	Trends       []PulseTrend
	Signal       []data.Article
	Sources      []PulseSource
}

// PulseDay counts articles published on one day, split between forums and everything else.
type PulseDay struct {
	Date   time.Time
	Forums int
	Other  int
}

// PulseTrend is a title word that shows up much more this week than usual.
type PulseTrend struct {
	Term  string
	Count int
	Lift  float64
}

// PulseSource describes the health of one configured feed.
type PulseSource struct {
	Name     string
	Path     string
	URL      string
	Total    int
	Month    int
	Last     time.Time
	Status   string
	Weeks    []int
	MaxWeeks int
}

const (
	pulseWeeks      = 12
	pulseDays       = 84
	pulseSignalSize = 24
	pulseTrendSize  = 24
)

var (
	pulseMu       sync.Mutex
	pulseCache    *Pulse
	pulseCacheKey string
)

// PulsePage renders the /pulse dashboard.
func PulsePage(c *gin.Context) {
	pulse, err := getPulse()
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}
	user, _ := GetCurrentUser(c)
	c.HTML(http.StatusOK, "pulse.html", gin.H{
		"pulse": pulse,
		"user":  user,
		"page":  "pulse",
		"path":  "",
	})
}

// getPulse returns the cached pulse, recomputing it when the posts or feeds files changed.
func getPulse() (*Pulse, error) {
	postsInfo, err := os.Stat(env.POSTS)
	if err != nil {
		return nil, fmt.Errorf("Error reading posts file: %v.", err)
	}
	feedsInfo, err := os.Stat(env.FEEDS)
	if err != nil {
		return nil, fmt.Errorf("Error reading feeds file: %v.", err)
	}
	key := postsInfo.ModTime().String() + "|" + feedsInfo.ModTime().String()

	pulseMu.Lock()
	defer pulseMu.Unlock()
	if pulseCache != nil && pulseCacheKey == key {
		return pulseCache, nil
	}

	var articles []data.Article
	file, err := os.ReadFile(env.POSTS)
	if err != nil {
		return nil, fmt.Errorf("Error reading posts file: %v.", err)
	}
	if err := json.Unmarshal(file, &articles); err != nil {
		return nil, fmt.Errorf("Error decoding posts: %v.", err)
	}

	var feeds []data.Rss
	file, err = os.ReadFile(env.FEEDS)
	if err != nil {
		return nil, fmt.Errorf("Error reading feeds file: %v.", err)
	}
	if err := json.Unmarshal(file, &feeds); err != nil {
		return nil, fmt.Errorf("Error decoding feeds: %v.", err)
	}

	pulseCache = computePulse(articles, feeds, time.Now())
	pulseCache.UpdatedAt = postsInfo.ModTime()
	pulseCacheKey = key
	return pulseCache, nil
}

// computePulse builds every section of the dashboard in a single pass over the articles.
func computePulse(articles []data.Article, feeds []data.Rss, now time.Time) *Pulse {
	p := &Pulse{UpdatedAt: now, Total: len(articles), TotalSources: len(feeds)}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekAgo := now.AddDate(0, 0, -7)
	twoWeeksAgo := now.AddDate(0, 0, -14)
	monthAgo := now.AddDate(0, 0, -30)
	baselineStart := now.AddDate(0, 0, -7-182)

	p.Days = make([]PulseDay, pulseDays)
	for i := range p.Days {
		p.Days[i].Date = today.AddDate(0, 0, i-pulseDays+1)
	}

	sources := make(map[string]*PulseSource, len(feeds))
	for _, feed := range feeds {
		sources[feed.Name] = &PulseSource{Name: feed.Name, Path: feed.Path, URL: feed.URL, Weeks: make([]int, pulseWeeks)}
	}

	weekTerms := map[string]int{}
	baseTerms := map[string]int{}
	baseCount := 0
	// Forums repost every reply under the same title: count each title once.
	seenTitles := map[string]bool{}
	weekTitles := 0

	for _, article := range articles {
		date := article.Date
		if date.After(now.Add(24 * time.Hour)) {
			continue // published after the pulse date (only happens when computing a past pulse)
		}
		if date.After(now) {
			date = now
		}

		if src, ok := sources[article.Source.Name]; ok {
			src.Total++
			if date.After(src.Last) {
				src.Last = date
			}
			if date.After(monthAgo) {
				src.Month++
			}
			if w := int(now.Sub(date).Hours() / (24 * 7)); w < pulseWeeks {
				src.Weeks[pulseWeeks-1-w]++
			}
		}

		if d := int(date.Sub(p.Days[0].Date).Hours() / 24); !date.Before(p.Days[0].Date) && d < pulseDays {
			if article.Source.Path == "forums" {
				p.Days[d].Forums++
			} else {
				p.Days[d].Other++
			}
		}

		switch {
		case date.After(weekAgo):
			p.Week++
			if title := strings.ToLower(article.Name); !seenTitles[title] {
				seenTitles[title] = true
				weekTitles++
				for term := range titleTerms(article.Name) {
					weekTerms[term]++
				}
			}
			if article.Source.Path != "forums" {
				p.SignalWeek++
				p.Signal = append(p.Signal, article)
			}
		case date.After(twoWeeksAgo):
			p.PrevWeek++
		}
		if title := strings.ToLower(article.Name); date.After(baselineStart) && !date.After(weekAgo) && !seenTitles["base|"+title] {
			seenTitles["base|"+title] = true
			baseCount++
			for term := range titleTerms(article.Name) {
				baseTerms[term]++
			}
		}
	}

	if p.PrevWeek > 0 {
		p.DeltaPct = int(math.Round(float64(p.Week-p.PrevWeek) / float64(p.PrevWeek) * 100))
	}

	for _, day := range p.Days {
		if total := day.Forums + day.Other; total > p.MaxDay {
			p.MaxDay = total
		}
	}

	// Trends: words whose share of this week's titles beats their 6-month baseline share.
	for term, count := range weekTerms {
		if count < max(2, weekTitles/60) {
			continue
		}
		expected := float64(baseTerms[term]) / float64(max(baseCount, 1)) * float64(weekTitles)
		lift := (float64(count) + 1) / (expected + 1)
		if lift < 2.5 {
			continue
		}
		p.Trends = append(p.Trends, PulseTrend{Term: term, Count: count, Lift: lift})
	}
	sort.Slice(p.Trends, func(i, j int) bool {
		si := float64(p.Trends[i].Count) * math.Log(p.Trends[i].Lift)
		sj := float64(p.Trends[j].Count) * math.Log(p.Trends[j].Lift)
		return si > sj
	})
	if len(p.Trends) > pulseTrendSize {
		p.Trends = p.Trends[:pulseTrendSize]
	}

	sort.SliceStable(p.Signal, func(i, j int) bool { return p.Signal[i].Date.After(p.Signal[j].Date) })
	if len(p.Signal) > pulseSignalSize {
		p.Signal = p.Signal[:pulseSignalSize]
	}

	for _, feed := range feeds {
		src := sources[feed.Name]
		switch age := now.Sub(src.Last); {
		case src.Total == 0:
			src.Status = "silent"
		case age < 14*24*time.Hour:
			src.Status = "live"
		case age < 90*24*time.Hour:
			src.Status = "quiet"
		default:
			src.Status = "dead"
		}
		if src.Status == "live" {
			p.LiveSources++
		}
		for _, n := range src.Weeks {
			src.MaxWeeks = max(src.MaxWeeks, n)
		}
		p.Sources = append(p.Sources, *src)
	}
	rank := map[string]int{"live": 0, "quiet": 1, "dead": 2, "silent": 3}
	sort.SliceStable(p.Sources, func(i, j int) bool {
		a, b := p.Sources[i], p.Sources[j]
		if rank[a.Status] != rank[b.Status] {
			return rank[a.Status] < rank[b.Status]
		}
		return a.Last.After(b.Last)
	})

	return p
}

// titleTerms returns the distinct, meaningful lowercase words of a title.
func titleTerms(title string) map[string]struct{} {
	terms := map[string]struct{}{}
	words := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '+'
	})
	for _, word := range words {
		if len(word) < 3 || pulseStopwords[word] || strings.Trim(word, "0123456789") == "" {
			continue
		}
		terms[word] = struct{}{}
	}
	return terms
}

var pulseStopwords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`the and for with from that this your you are how what why when who which
		can not but all any was were has have had its it's into out about over than then them they their there
		use using used new get got one two way ways should would could does did doing done just like more most
		make makes making need needs want help best good better vs via any some only also very much many own
		i'm don't doesn't isn't can't won't i've let's what's here where while after before between each other
		golang go's gopher gophers question questions anyone someone something thing things work working works
		code app project projects post posts article week weekly issue looking built build building write writing
		without within under first last still really yet our ours his her him she will been being too same
		coding programming programmer developer developers dev show tell
		actually hard few back day days approach path solve requested noob struggling automatically per add
		next across through every since even well already never always again around enough going thoughts
		advice opinion opinions feedback recommendation recommendations learn learning beginner tips idea ideas`) {
		m[w] = true
	}
	return m
}()

// pulseFuncs are template helpers used by pulse.html.
var pulseFuncs = map[string]any{
	"ago": func(t time.Time) string {
		if t.IsZero() {
			return "never"
		}
		d := time.Since(t)
		switch {
		case d < time.Hour:
			return fmt.Sprintf("%dm ago", int(d.Minutes()))
		case d < 48*time.Hour:
			return fmt.Sprintf("%dh ago", int(d.Hours()))
		case d < 60*24*time.Hour:
			return fmt.Sprintf("%dd ago", int(d.Hours()/24))
		case d < 730*24*time.Hour:
			return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
		}
		return fmt.Sprintf("%dy ago", int(d.Hours()/24/365))
	},
	"pct": func(n, total int) int {
		if total == 0 {
			return 0
		}
		return n * 100 / total
	},
	"liftf": func(f float64) string { return fmt.Sprintf("×%.1f", f) },
	"add":   func(a, b int) int { return a + b },
}
