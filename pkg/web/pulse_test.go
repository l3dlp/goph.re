package web

import (
	"gophre/cmd/data"
	"testing"
	"time"
)

func TestComputePulse(t *testing.T) {
	now := time.Now()
	feeds := []data.Rss{{Path: "golang", Name: "Go"}, {Path: "forums", Name: "Forum"}, {Path: "blogs", Name: "Gone"}}
	articles := []data.Article{
		{Name: "Generics are here", Source: feeds[0], Date: now.Add(-time.Hour)},
		{Name: "Generics help needed", Source: feeds[1], Date: now.Add(-2 * time.Hour)},
		{Name: "Generics help needed", Source: feeds[1], Date: now.Add(-3 * time.Hour)},
		{Name: "Old release notes", Source: feeds[0], Date: now.AddDate(0, 0, -10)},
	}
	p := computePulse(articles, feeds, now)

	if p.Week != 3 || p.PrevWeek != 1 || p.SignalWeek != 1 {
		t.Fatalf("week=%d prev=%d signal=%d, want 3/1/1", p.Week, p.PrevWeek, p.SignalWeek)
	}
	if len(p.Trends) == 0 || p.Trends[0].Term != "generics" || p.Trends[0].Count != 2 {
		t.Fatalf("trends=%+v, want generics counted once per distinct title", p.Trends)
	}
	if got := p.Days[len(p.Days)-1].Forums + p.Days[len(p.Days)-1].Other; got > 3 {
		t.Fatalf("today counted %d articles", got)
	}
	status := map[string]string{}
	for _, s := range p.Sources {
		status[s.Name] = s.Status
	}
	if status["Go"] != "live" || status["Gone"] != "silent" {
		t.Fatalf("statuses=%v", status)
	}
}
