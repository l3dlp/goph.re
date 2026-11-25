// Package push stores Web Push subscriptions and sends notifications to them.
package push

import (
	"encoding/json"
	"errors"
	"fmt"
	"gophre/cmd/data"
	"gophre/env"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Topics a subscriber can follow.
const (
	TopicOfficial = "official" // go.dev, releases, Golang Weekly
	TopicSignal   = "signal"   // every source outside the forums
)

// MaxSubscriptions caps the store so an abusive client cannot grow it forever.
const MaxSubscriptions = 20000

// Subscription is one browser endpoint and the topics it follows.
type Subscription struct {
	Endpoint string    `json:"endpoint"`
	Keys     Keys      `json:"keys"`
	Topics   []string  `json:"topics"`
	UserID   string    `json:"user_id,omitempty"`
	Created  time.Time `json:"created"`
}

// Keys are the browser's encryption keys for a subscription.
type Keys struct {
	P256dh string `json:"p256dh"`
	Auth   string `json:"auth"`
}

// Payload is the JSON the service worker receives in its push event.
type Payload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag,omitempty"`
	Count int    `json:"count,omitempty"`
	Image string `json:"image,omitempty"`
}

var (
	mu        sync.Mutex
	storePath = env.PATH + "push/subscriptions.json"
)

// Enabled reports whether VAPID keys are configured.
func Enabled() bool {
	return env.VAPID_PUBLIC_KEY != "" && env.VAPID_PRIVATE_KEY != ""
}

// ValidTopic reports whether t is a known topic.
func ValidTopic(t string) bool {
	return t == TopicOfficial || t == TopicSignal
}

func load() ([]Subscription, error) {
	file, err := os.ReadFile(storePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Error reading subscriptions: %v.", err)
	}
	var subs []Subscription
	if err := json.Unmarshal(file, &subs); err != nil {
		return nil, fmt.Errorf("Error decoding subscriptions: %v.", err)
	}
	return subs, nil
}

// save writes atomically so the web server and the cron never see a half-written file.
func save(subs []Subscription) error {
	if err := os.MkdirAll(filepath.Dir(storePath), 0750); err != nil {
		return fmt.Errorf("Error creating push directory: %v.", err)
	}
	encoded, err := json.MarshalIndent(subs, "", "  ")
	if err != nil {
		return fmt.Errorf("Error encoding subscriptions: %v.", err)
	}
	tmp := storePath + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0640); err != nil {
		return fmt.Errorf("Error writing subscriptions: %v.", err)
	}
	return os.Rename(tmp, storePath)
}

// Subscribe adds or updates a subscription; an empty topic list removes it.
func Subscribe(sub Subscription) error {
	mu.Lock()
	defer mu.Unlock()
	subs, err := load()
	if err != nil {
		return err
	}
	kept := subs[:0]
	var previous *Subscription
	for i := range subs {
		if subs[i].Endpoint == sub.Endpoint {
			previous = &subs[i]
			continue
		}
		kept = append(kept, subs[i])
	}
	if len(sub.Topics) > 0 {
		if previous == nil && len(kept) >= MaxSubscriptions {
			return errors.New("Too many subscriptions.")
		}
		sub.Created = time.Now()
		if previous != nil {
			sub.Created = previous.Created
		}
		kept = append(kept, sub)
	}
	return save(kept)
}

// Unsubscribe removes the subscription with this endpoint.
func Unsubscribe(endpoint string) error {
	return Subscribe(Subscription{Endpoint: endpoint})
}

// Count returns the number of stored subscriptions.
func Count() int {
	mu.Lock()
	defer mu.Unlock()
	subs, _ := load()
	return len(subs)
}

// SendTo delivers one payload to one subscription and reports whether the endpoint is gone.
func SendTo(sub Subscription, p Payload) (gone bool, err error) {
	body, err := json.Marshal(p)
	if err != nil {
		return false, err
	}
	resp, err := webpush.SendNotification(body, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.Keys.P256dh, Auth: sub.Keys.Auth},
	}, &webpush.Options{
		Subscriber:      env.VAPID_SUBJECT,
		VAPIDPublicKey:  env.VAPID_PUBLIC_KEY,
		VAPIDPrivateKey: env.VAPID_PRIVATE_KEY,
		TTL:             12 * 3600,
		Urgency:         webpush.UrgencyNormal,
		Topic:           "gophre-new",
	})
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return true, nil
	case resp.StatusCode >= 400:
		return false, fmt.Errorf("push service answered %s", resp.Status)
	}
	return false, nil
}

// Broadcast sends a payload built per subscription; build returns false to skip one.
// Expired endpoints are pruned from the store.
func Broadcast(build func(Subscription) (Payload, bool)) (sent int) {
	if !Enabled() {
		return 0
	}
	mu.Lock()
	subs, err := load()
	mu.Unlock()
	if err != nil {
		log.Println(err)
		return 0
	}

	var gone []string
	for _, sub := range subs {
		p, ok := build(sub)
		if !ok {
			continue
		}
		expired, err := SendTo(sub, p)
		switch {
		case expired:
			gone = append(gone, sub.Endpoint)
		case err != nil:
			log.Printf("Error sending push to %s: %v.\n", host(sub.Endpoint), err)
		default:
			sent++
		}
	}
	for _, endpoint := range gone {
		if err := Unsubscribe(endpoint); err != nil {
			log.Println(err)
		}
	}
	return sent
}

// NotifyNew sends one grouped notification per subscriber about freshly fetched articles.
func NotifyNew(articles []data.Article) {
	if len(articles) == 0 || !Enabled() {
		return
	}
	sort.SliceStable(articles, func(i, j int) bool { return articles[i].Date.After(articles[j].Date) })
	sent := Broadcast(func(sub Subscription) (Payload, bool) {
		var picked []data.Article
		for _, a := range articles {
			if matches(sub.Topics, a) {
				picked = append(picked, a)
			}
		}
		if len(picked) == 0 {
			return Payload{}, false
		}
		return summarize(picked), true
	})
	log.Printf("Push: %d new articles, %d notifications sent.\n", len(articles), sent)
}

func matches(topics []string, a data.Article) bool {
	for _, t := range topics {
		switch {
		case t == TopicSignal && a.Source.Path != "forums":
			return true
		case t == TopicOfficial && (a.Source.Path == "golang" || a.Source.Path == "official" || a.Source.Path == "picks"):
			return true
		}
	}
	return false
}

func summarize(picked []data.Article) Payload {
	if len(picked) == 1 {
		a := picked[0]
		return Payload{Title: a.Source.Name, Body: a.Name, URL: a.URL, Tag: "gophre-new", Count: 1}
	}
	lines := []string{}
	for i, a := range picked {
		if i == 3 {
			lines = append(lines, fmt.Sprintf("+ %d more", len(picked)-3))
			break
		}
		lines = append(lines, "• "+a.Name)
	}
	return Payload{
		Title: fmt.Sprintf("%d new on goph.re", len(picked)),
		Body:  strings.Join(lines, "\n"),
		URL:   "/",
		Tag:   "gophre-new",
		Count: len(picked),
	}
}

func host(endpoint string) string {
	endpoint = strings.TrimPrefix(endpoint, "https://")
	if i := strings.IndexByte(endpoint, '/'); i > 0 {
		return endpoint[:i]
	}
	return endpoint
}
