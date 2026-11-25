package push

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"gophre/cmd/data"
	"gophre/env"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// browserKeys returns a valid p256dh/auth pair, as a browser would generate.
func browserKeys(t *testing.T) Keys {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	return Keys{
		P256dh: base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
		Auth:   base64.RawURLEncoding.EncodeToString(auth),
	}
}

func TestNotifyNewRoutesTopicsAndPrunesGoneEndpoints(t *testing.T) {
	storePath = filepath.Join(t.TempDir(), "subscriptions.json")
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	env.VAPID_PUBLIC_KEY, env.VAPID_PRIVATE_KEY, env.VAPID_SUBJECT = public, private, "https://goph.re"

	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Content-Encoding") != "aes128gcm" || len(body) == 0 {
			t.Errorf("payload not encrypted: %q", r.Header.Get("Content-Encoding"))
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") || !strings.Contains(r.Header.Get("Authorization"), "k="+public) {
			t.Errorf("missing VAPID authorization: %q", r.Header.Get("Authorization"))
		}
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		if r.URL.Path == "/gone" {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	for _, s := range []Subscription{
		{Endpoint: srv.URL + "/official", Keys: browserKeys(t), Topics: []string{TopicOfficial}},
		{Endpoint: srv.URL + "/signal", Keys: browserKeys(t), Topics: []string{TopicSignal}},
		{Endpoint: srv.URL + "/gone", Keys: browserKeys(t), Topics: []string{TopicSignal}},
	} {
		if err := Subscribe(s); err != nil {
			t.Fatal(err)
		}
	}

	NotifyNew([]data.Article{
		{Name: "a forum thread", Source: data.Rss{Path: "forums"}, Date: time.Now()},
		{Name: "an expert post", Source: data.Rss{Path: "experts"}, Date: time.Now()},
	})

	if hits["/official"] != 0 {
		t.Errorf("official subscriber was notified about non-official articles")
	}
	if hits["/signal"] != 1 || hits["/gone"] != 1 {
		t.Errorf("hits = %v, want one push to /signal and /gone", hits)
	}
	if n := Count(); n != 2 {
		t.Errorf("Count() = %d after a 410, want 2 (gone endpoint pruned)", n)
	}

	NotifyNew([]data.Article{{Name: "go1.27.4", Source: data.Rss{Path: "golang", Name: "Go Releases"}, Date: time.Now()}})
	if hits["/official"] != 1 || hits["/signal"] != 2 {
		t.Errorf("hits = %v after a release, want official=1 signal=2", hits)
	}
}

func TestSummarize(t *testing.T) {
	one := summarize([]data.Article{{Name: "go1.27.4", URL: "https://go.dev/x", Source: data.Rss{Name: "Go Releases"}}})
	if one.Title != "Go Releases" || one.URL != "https://go.dev/x" || one.Count != 1 {
		t.Errorf("single = %+v", one)
	}
	many := summarize(make([]data.Article, 5))
	if many.Title != "5 new on goph.re" || many.URL != "/" || !strings.Contains(many.Body, "+ 2 more") {
		t.Errorf("many = %+v", many)
	}
}
