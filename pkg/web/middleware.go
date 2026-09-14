package web

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

const (
	// limiterCleanupInterval is how often idle visitor entries are scanned.
	limiterCleanupInterval = 5 * time.Minute

	// limiterMaxIdle is how long a visitor entry may stay unused before it is
	// evicted. An evicted visitor simply gets a fresh limiter on its next
	// request, so eviction never grants more burst than a new visitor gets.
	limiterMaxIdle = 15 * time.Minute
)

// visitorLimiter pairs a visitor's rate limiter with its last activity time.
type visitorLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPRateLimiter stores rate limiters for each visitor
type IPRateLimiter struct {
	ips map[string]*visitorLimiter
	mu  sync.Mutex
	r   rate.Limit
	b   int
}

// NewLimiter creates a new rate limiter and starts a janitor goroutine that
// evicts entries idle for longer than limiterMaxIdle.
func NewLimiter(r rate.Limit, b int) *IPRateLimiter {
	limiter := &IPRateLimiter{
		ips: make(map[string]*visitorLimiter),
		r:   r,
		b:   b,
	}

	go limiter.cleanup()

	return limiter
}

// GetLimiter returns the rate limiter for the provided IP address
func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	visitor, exists := i.ips[ip]
	if !exists {
		visitor = &visitorLimiter{limiter: rate.NewLimiter(i.r, i.b)}
		i.ips[ip] = visitor
	}
	visitor.lastSeen = time.Now()

	return visitor.limiter
}

// cleanup periodically removes visitors that have been idle for too long so
// the map does not grow forever.
func (i *IPRateLimiter) cleanup() {
	for range time.Tick(limiterCleanupInterval) {
		i.mu.Lock()
		for ip, visitor := range i.ips {
			if time.Since(visitor.lastSeen) > limiterMaxIdle {
				delete(i.ips, ip)
			}
		}
		i.mu.Unlock()
	}
}

// RateLimiterMiddleware is a middleware that limits request rate
func RateLimiterMiddleware(limiter *IPRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		limiter := limiter.GetLimiter(ip)
		if !limiter.Allow() {
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		c.Next()
	}
}
