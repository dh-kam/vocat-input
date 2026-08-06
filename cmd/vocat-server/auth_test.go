package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() { gin.SetMode(gin.TestMode) }

// withSessionSecret sets the package-level secret for one test and restores it afterwards.
func withSessionSecret(t *testing.T, secret string) {
	t.Helper()
	orig := sessionSecret
	sessionSecret = secret
	t.Cleanup(func() { sessionSecret = orig })
}

func TestResolveSessionSecret_PrefersExplicitValue(t *testing.T) {
	t.Setenv("VOCAT_SESSION_SECRET", "from-environment")
	got, err := resolveSessionSecret(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, "from-environment", got)
}

// No configuration must not mean a known default: the previous fallback was a literal committed
// in this file and shipped inside the web bundle.
func TestResolveSessionSecret_GeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()

	first, err := resolveSessionSecret(dir)
	require.NoError(t, err)
	assert.Len(t, first, 64, "expected 32 random bytes as hex")
	assert.NotEqual(t, "vocat_secure_session_secret_2026", first)

	path := filepath.Join(dir, "session_secret")
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the secret file must not be world readable")

	// Reused on the next start, otherwise every open tab's cookie is invalidated on restart.
	second, err := resolveSessionSecret(dir)
	require.NoError(t, err)
	assert.Equal(t, first, second)
}

func TestResolveSessionSecret_IgnoresABlankFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "session_secret"), []byte("  \n"), 0o600))

	got, err := resolveSessionSecret(dir)
	require.NoError(t, err)
	assert.Len(t, got, 64)
}

func requestWith(t *testing.T, apply func(*http.Request)) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodGet, "/api/runs", nil)
	apply(req)
	c.Request = req
	return c
}

func TestIsAuthenticated_AcceptsEveryChannel(t *testing.T) {
	withSessionSecret(t, "the-secret")

	cases := map[string]func(*http.Request){
		"session cookie":  func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "vocat_session", Value: "the-secret"}) },
		"bearer header":   func(r *http.Request) { r.Header.Set("Authorization", "Bearer the-secret") },
		"x-vocat-session": func(r *http.Request) { r.Header.Set("X-Vocat-Session", "the-secret") },
	}

	for name, apply := range cases {
		t.Run(name, func(t *testing.T) {
			assert.True(t, isAuthenticated(requestWith(t, apply)))
		})
	}
}

func TestIsAuthenticated_RejectsEverythingElse(t *testing.T) {
	withSessionSecret(t, "the-secret")

	cases := map[string]func(*http.Request){
		"nothing":                 func(*http.Request) {},
		"old hardcoded default":   func(r *http.Request) { r.Header.Set("X-Vocat-Session", "vocat_secure_session_secret_2026") },
		"wrong cookie":            func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "vocat_session", Value: "nope"}) },
		"empty cookie":            func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "vocat_session", Value: ""}) },
		"empty header":            func(r *http.Request) { r.Header.Set("X-Vocat-Session", "") },
		"bearer without a prefix": func(r *http.Request) { r.Header.Set("Authorization", "the-secret") },
		"prefix of the secret":    func(r *http.Request) { r.Header.Set("X-Vocat-Session", "the-secr") },
	}

	for name, apply := range cases {
		t.Run(name, func(t *testing.T) {
			assert.False(t, isAuthenticated(requestWith(t, apply)))
		})
	}
}

// An empty configured secret must never authenticate a credential-less request.
func TestIsAuthenticated_EmptySecretAuthenticatesNothing(t *testing.T) {
	withSessionSecret(t, "")
	assert.False(t, isAuthenticated(requestWith(t, func(*http.Request) {})))
	assert.False(t, isAuthenticated(requestWith(t, func(r *http.Request) { r.Header.Set("X-Vocat-Session", "") })))
}

// Serving the app is what grants a browser its session, and the cookie must be out of reach of
// scripts on the origin — the previous one was set with httpOnly false.
func TestServeSPA_IssuesAnHttpOnlySessionCookie(t *testing.T) {
	withSessionSecret(t, "the-secret")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644))

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	serveSPA(dir)(c)

	res := rec.Result()
	defer res.Body.Close()

	var session *http.Cookie
	for _, cookie := range res.Cookies() {
		if cookie.Name == "vocat_session" {
			session = cookie
		}
	}
	require.NotNil(t, session, "serving the app must issue a session cookie")
	assert.Equal(t, "the-secret", session.Value)
	assert.True(t, session.HttpOnly, "the cookie must not be readable by scripts")
	assert.Equal(t, http.SameSiteLaxMode, session.SameSite)
	assert.Equal(t, "/", session.Path)
	assert.False(t, session.Secure, "plain HTTP request should not be marked secure")
}
