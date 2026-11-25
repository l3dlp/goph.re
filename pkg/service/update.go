package service

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"gophre/cmd/data"
	"gophre/env"
	"gophre/pkg/push"
	"gophre/pkg/store"
	"html"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/mmcdole/gofeed"
)

const (
	// discordContentLimit is Discord's hard cap on a message's content field.
	discordContentLimit = 2000

	// discordMinInterval throttles webhook calls to stay under Discord's rate limit.
	discordMinInterval = 1500 * time.Millisecond

	// notifyMaxAge limits Discord notifications to recently published articles so a
	// backfill of old feed items does not flood the channel.
	notifyMaxAge = 7 * 24 * time.Hour

	// pushMaxAge is tighter: a push should only ever announce genuinely new articles.
	pushMaxAge = 48 * time.Hour
)

// discordStripHTML removes all markup from feed descriptions before posting.
var discordStripHTML = bluemonday.StrictPolicy()

// lastDiscordCall tracks the last webhook call for simple client-side throttling.
var lastDiscordCall time.Time

// truncate shortens s to at most n runes, appending an ellipsis when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// plainText strips all HTML markup and entities from s, returning collapsed
// raw text suitable for a Discord message.
func plainText(s string) string {
	// Strip tags, then unescape entities, then strip again in case an entity
	// such as "&lt;b&gt;" decoded into a real tag.
	s = discordStripHTML.Sanitize(s)
	s = html.UnescapeString(s)
	s = discordStripHTML.Sanitize(s)
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

// notifyDiscord posts the given article to the Discord webhook configured via
// DISCORD_WEBHOOK_URL, sanitizing and truncating the content and respecting
// Discord's rate limits. It is a no-op when no webhook is configured.
func notifyDiscord(article data.Article) {
	if env.DISCORD_WEBHOOK_URL == "" {
		return
	}

	title := plainText(article.Name)
	resume := plainText(article.Resume)

	var b strings.Builder
	if title != "" {
		b.WriteString("**" + title + "**\n")
	}
	if resume != "" {
		b.WriteString(resume + "\n")
	}
	b.WriteString(article.URL)

	content := truncate(strings.TrimSpace(b.String()), discordContentLimit)
	if content == "" {
		return // Nothing meaningful to post; avoids a guaranteed 400.
	}

	payload, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		log.Printf("Error encoding Discord payload: %v\n", err)
		return
	}

	// Up to 5 attempts so a 429 can be retried after the advised delay.
	for attempt := 0; attempt < 5; attempt++ {
		if wait := discordMinInterval - time.Since(lastDiscordCall); wait > 0 {
			time.Sleep(wait)
		}

		resp, err := http.Post(env.DISCORD_WEBHOOK_URL, "application/json", bytes.NewReader(payload))
		lastDiscordCall = time.Now()
		if err != nil {
			log.Printf("Error calling Discord webhook: %v\n", err)
			return
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			wait := discordRetryAfter(resp)
			resp.Body.Close()
			log.Printf("Discord rate limited, retrying in %s\n", wait)
			time.Sleep(wait)
			continue
		}

		if resp.StatusCode >= 300 {
			log.Printf("Discord webhook returned status: %s\n", resp.Status)
		}
		resp.Body.Close()
		return
	}

	log.Printf("Discord webhook gave up after repeated rate limiting for: %s\n", article.URL)
}

// discordRetryAfter reads the retry delay advised by a 429 response, falling
// back to the client throttle interval when no hint is provided.
func discordRetryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
			return time.Duration(secs * float64(time.Second))
		}
	}
	var body struct {
		RetryAfter float64 `json:"retry_after"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.RetryAfter > 0 {
		return time.Duration(body.RetryAfter * float64(time.Second))
	}
	return discordMinInterval
}

func Update() {
	// Charger les sources RSS à partir d'un fichier JSON
	file, err := os.ReadFile(env.FEEDS)
	if err != nil {
		fmt.Println("Erreur lors de la lecture du fichier :", err)
		return
	}

	var sources []data.Rss
	err = json.Unmarshal(file, &sources)
	if err != nil {
		fmt.Println("Erreur lors de la décodage du JSON :", err)
		return
	}

	// Récupérer les articles de chaque source RSS
	var fresh []data.Article
	for _, src := range sources {
		fmt.Printf("%s:\n", src.Name)
		fresh = append(fresh, readSource(src)...)
	}

	// One grouped Web Push notification per subscriber for this run.
	push.NotifyNew(fresh)
}

func parseDate(dateString string, layouts []string) (time.Time, error) {
	var parsedDate time.Time
	var err error

	for _, layout := range layouts {
		parsedDate, err = time.Parse(layout, dateString)
		if err == nil {
			return parsedDate, nil
		}
	}

	return parsedDate, err
}

// itemDate resolves a feed item's date, preferring gofeed's already-parsed
// values (which handle GMT, MST and many other layouts) and falling back to the
// raw string with our custom layouts. Returns the zero time when unknown.
func itemDate(item *gofeed.Item, layouts []string) time.Time {
	if item.PublishedParsed != nil {
		return *item.PublishedParsed
	}
	if item.UpdatedParsed != nil {
		return *item.UpdatedParsed
	}

	raw := strings.TrimSpace(item.Published)
	if raw == "" {
		raw = strings.TrimSpace(item.Updated)
	}
	if raw == "" {
		return time.Time{}
	}

	parsed, err := parseDate(raw, layouts)
	if err != nil {
		log.Printf("Error parsing published date %q: %v\n", raw, err)
		return time.Time{}
	}
	return parsed
}

// readSource stores a feed's new items and returns the ones published recently
// enough to push; recent ones are also announced on Discord.
func readSource(source data.Rss) (fresh []data.Article) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURL(source.URL)
	if err != nil {
		log.Printf("Error parsing URL %s: %v\n", source.URL, err)
		return
	}

	customRFC := "Mon, 2 Jan 2006 15:04:05"
	customRFC1123Z := "Mon, 2 Jan 2006 15:04:05 -0700"
	layouts := []string{
		customRFC,
		time.RFC1123,
		time.RFC1123Z,
		time.RFC3339,
		customRFC1123Z,
	}

	var candidates []data.Article
	for _, item := range feed.Items {
		hash := md5.Sum([]byte(item.Title + "." + item.Link))
		candidates = append(candidates, data.Article{
			ID:     hex.EncodeToString(hash[:]),
			Source: source,
			Name:   item.Title,
			URL:    item.Link,
			Resume: item.Description,
			Date:   itemDate(item, layouts),
		})
	}

	// The store skips URLs it already knows and returns only the new articles.
	newArticles, err := store.Insert(candidates)
	if err != nil {
		log.Printf("Error storing articles from %s: %v\n", source.Name, err)
		return
	}

	for _, article := range newArticles {
		// Only notify for genuinely recent items so a one-off backfill of old
		// feed entries does not flood Discord.
		if !article.Date.IsZero() && time.Since(article.Date) <= notifyMaxAge {
			notifyDiscord(article)
		}
		if !article.Date.IsZero() && time.Since(article.Date) <= pushMaxAge {
			fresh = append(fresh, article)
		}
	}
	return fresh
}
