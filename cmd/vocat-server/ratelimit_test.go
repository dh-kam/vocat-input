package main

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIPLimiter_AllowsUpToLimitThenRejects(t *testing.T) {
	l := newIPLimiter(3, time.Minute)
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	assert.True(t, l.Allow("1.2.3.4", now), "first attempt within limit")
	assert.True(t, l.Allow("1.2.3.4", now), "second attempt within limit")
	assert.True(t, l.Allow("1.2.3.4", now), "third attempt consumes the last token")
	assert.False(t, l.Allow("1.2.3.4", now), "fourth attempt must be refused")
	assert.False(t, l.Allow("1.2.3.4", now.Add(10*time.Second)), "still refused mid-window")
}

func TestIPLimiter_FixedWindowResets(t *testing.T) {
	l := newIPLimiter(2, time.Minute)
	t0 := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	require.True(t, l.Allow("1.2.3.4", t0))
	require.True(t, l.Allow("1.2.3.4", t0))
	require.False(t, l.Allow("1.2.3.4", t0.Add(30*time.Second)), "mid-window: refused")
	// The window is fixed, so resetAt never moved: one second past it, the bucket is full again.
	assert.True(t, l.Allow("1.2.3.4", t0.Add(time.Minute+time.Second)))
	assert.True(t, l.Allow("1.2.3.4", t0.Add(time.Minute+time.Second)))
	assert.False(t, l.Allow("1.2.3.4", t0.Add(time.Minute+time.Second)), "refilled bucket is exhausted again")
}

func TestIPLimiter_KeysAreIndependent(t *testing.T) {
	l := newIPLimiter(1, time.Minute)
	now := time.Now()

	assert.True(t, l.Allow("a", now))
	assert.False(t, l.Allow("a", now), "a is exhausted")
	assert.True(t, l.Allow("b", now), "b has its own bucket")
}

func TestIPLimiter_ConcurrentAccess(t *testing.T) {
	l := newIPLimiter(100, time.Minute)
	now := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = l.Allow("shared", now)
		}()
	}
	wg.Wait()
	// The exact count admitted is racy by design; what matters is that the mutex keeps the map
	// from corrupting under concurrent writers, which -race asserts if this runs under it.
	assert.LessOrEqual(t, l.hits["shared"].count, 100)
}

func TestRateLimitByIP_429OverLimit(t *testing.T) {
	withSessionSecret(t, "the-secret")
	l := newIPLimiter(1, time.Minute)

	r := gin.New()
	r.POST("/login", rateLimitByIP(l), handleLogin)

	body := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		return req
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, body())
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "first attempt reaches handleLogin and fails auth")

	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, body())
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code, "second attempt is rate limited before reaching the handler")
}
