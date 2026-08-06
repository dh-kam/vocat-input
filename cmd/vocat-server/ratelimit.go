package main

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ipLimiter is a fixed-window, per-key throttle. The login endpoint accepts a 256-bit random
// secret, so brute force is not the realistic threat; the limiter exists to stop a flood of
// guesses from being a cheap CPU and log amplifier and to add defense in depth should the secret
// ever be weakened. It is deliberately in-process: this server is single-binary, and a shared
// bucket per restart is acceptable.
type ipLimiter struct {
	mu     sync.Mutex
	window time.Duration
	limit  int
	hits   map[string]ipWindow
}

type ipWindow struct {
	count   int
	resetAt time.Time
}

func newIPLimiter(limit int, window time.Duration) *ipLimiter {
	return &ipLimiter{window: window, limit: limit, hits: make(map[string]ipWindow)}
}

// Allow consumes one token for key at instant now and reports whether it fell within the limit.
// now is a parameter rather than a time.Now() call inside so a test can advance the clock.
//
// This is a fixed window: once established, resetAt does not move, so a caller who keeps hammering
// is still refused on the original schedule and a legitimate user who backs off is back in as soon
// as the window rolls over. Extending resetAt on every rejection was rejected because it lets a
// user lock themselves out indefinitely by retrying, and the 256-bit secret means a sustained
// burst buys an attacker nothing anyway.
func (l *ipLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	w, ok := l.hits[key]
	if !ok || !now.Before(w.resetAt) {
		l.hits[key] = ipWindow{count: 1, resetAt: now.Add(l.window)}
		return l.limit >= 1
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	l.hits[key] = w
	return true
}

// rateLimitByIP gates a route on the per-IP bucket. c.ClientIP() honors gin's trusted-proxy
// setting, which defaults to trusting no proxies, so a remote cannot forge an X-Forwarded-For to
// spread its attempts across synthetic addresses.
func rateLimitByIP(limiter *ipLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limiter.Allow(c.ClientIP(), time.Now()) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many attempts. Please try again later.",
				"code":  "RATE_LIMITED",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
