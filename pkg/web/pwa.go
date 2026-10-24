package web

import (
	"gophre/env"
	"gophre/pkg/push"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// SetupPWA registers the manifest, service worker, offline page, icons and Web Push endpoints.
func SetupPWA(r *gin.Engine) {
	r.GET("/sw.js", func(c *gin.Context) {
		// Served from the root so the worker controls the whole site.
		c.Header("Service-Worker-Allowed", "/")
		c.Header("Cache-Control", "no-cache")
		c.Header("Content-Type", "text/javascript; charset=utf-8")
		c.File(env.PATH + "assets/js/sw.js")
	})
	r.GET("/manifest.webmanifest", func(c *gin.Context) {
		c.Header("Content-Type", "application/manifest+json")
		c.Header("Cache-Control", "public, max-age=3600")
		c.File(env.PATH + "assets/manifest.webmanifest")
	})
	r.GET("/favicon.ico", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=604800")
		c.File(env.PATH + "assets/favicon.ico")
	})
	r.GET("/robots.txt", func(c *gin.Context) {
		c.String(http.StatusOK, "User-agent: *\nAllow: /\n")
	})
	r.GET("/offline", func(c *gin.Context) {
		c.HTML(http.StatusOK, "offline.html", gin.H{"meta": metaFor("feed", "", false)})
	})

	r.GET("/push/key", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"enabled": push.Enabled(), "key": env.VAPID_PUBLIC_KEY})
	})
	r.POST("/push/subscribe", PushSubscribe)
	r.POST("/push/unsubscribe", PushUnsubscribe)
}

type pushRequest struct {
	Endpoint string    `json:"endpoint"`
	Keys     push.Keys `json:"keys"`
	Topics   []string  `json:"topics"`
}

func validPushEndpoint(e string) bool {
	return strings.HasPrefix(e, "https://") && len(e) < 2048
}

// PushSubscribe stores (or updates the topics of) a browser push subscription.
func PushSubscribe(c *gin.Context) {
	if !push.Enabled() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "Push is not configured."})
		return
	}
	var req pushRequest
	if err := c.ShouldBindJSON(&req); err != nil || !validPushEndpoint(req.Endpoint) ||
		req.Keys.P256dh == "" || req.Keys.Auth == "" || len(req.Keys.P256dh) > 256 || len(req.Keys.Auth) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid subscription."})
		return
	}
	topics := []string{}
	seen := map[string]bool{}
	for _, t := range req.Topics {
		if push.ValidTopic(t) && !seen[t] {
			seen[t] = true
			topics = append(topics, t)
		}
	}
	user, _ := GetCurrentUser(c)
	sub := push.Subscription{Endpoint: req.Endpoint, Keys: req.Keys, Topics: topics, UserID: user.ID}
	if err := push.Subscribe(sub); err != nil {
		log.Printf("Error saving push subscription: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Could not save the subscription."})
		return
	}
	if len(topics) > 0 && c.Query("welcome") == "1" {
		go func() {
			if _, err := push.SendTo(sub, push.Payload{
				Title: "goph.re notifications are on",
				Body:  "Next time something worth reading lands in Go, you'll see it here.",
				URL:   "/pulse",
				Tag:   "gophre-welcome",
			}); err != nil {
				log.Printf("Error sending welcome push: %v\n", err)
			}
		}()
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "topics": topics})
}

// PushUnsubscribe forgets a browser push subscription.
func PushUnsubscribe(c *gin.Context) {
	var req pushRequest
	if err := c.ShouldBindJSON(&req); err != nil || !validPushEndpoint(req.Endpoint) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "Invalid subscription."})
		return
	}
	if err := push.Unsubscribe(req.Endpoint); err != nil {
		log.Printf("Error removing push subscription: %v\n", err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "Could not remove the subscription."})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
