package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func withAdminPassword(t *testing.T, password string) {
	t.Helper()
	orig := adminPassword
	adminPassword = password
	t.Cleanup(func() { adminPassword = orig })
}

func TestIsAuthenticated_AcceptsEveryChannel(t *testing.T) {
	withSessionSecret(t, "the-secret")
	tok, err := mintSessionToken()
	require.NoError(t, err)

	cases := map[string]func(*http.Request){
		"derived session cookie": func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "vocat_session", Value: tok}) },
		"bearer header secret":   func(r *http.Request) { r.Header.Set("Authorization", "Bearer the-secret") },
		"bearer derived token":   func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) },
		"x-vocat-session secret": func(r *http.Request) { r.Header.Set("X-Vocat-Session", "the-secret") },
	}

	for name, apply := range cases {
		t.Run(name, func(t *testing.T) {
			assert.True(t, isAuthenticated(requestWith(t, apply)))
		})
	}
}

func TestIsAuthenticated_CookieRejectsMasterSecret(t *testing.T) {
	withSessionSecret(t, "the-secret")
	assert.False(t, isAuthenticated(requestWith(t, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "vocat_session", Value: "the-secret"})
	})), "the raw session secret must not work as a cookie")
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
	assert.NotEqual(t, "the-secret", session.Value, "cookie must be a derived token, not the master secret")
	assert.True(t, validSessionToken(session.Value), "issued cookie must verify against the session secret")
	assert.True(t, session.HttpOnly, "the cookie must not be readable by scripts")
	assert.Equal(t, http.SameSiteLaxMode, session.SameSite)
	assert.Equal(t, "/", session.Path)
	assert.False(t, session.Secure, "plain HTTP request should not be marked secure")
}

func TestServeSPA_AuthRequiredMode_DoesNotIssueCookie(t *testing.T) {
	withSessionSecret(t, "the-secret")
	origAuth := authRequired
	authRequired = true
	defer func() { authRequired = origAuth }()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html></html>"), 0o644))

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	serveSPA(dir)(c)

	res := rec.Result()
	defer res.Body.Close()

	for _, cookie := range res.Cookies() {
		if cookie.Name == "vocat_session" {
			t.Fatalf("authRequired mode must NOT issue auto session cookie on GET /")
		}
	}
}

func TestIsAllowedOrigin(t *testing.T) {
	cases := map[string]struct {
		origin string
		allow  bool
	}{
		"empty":                    {"", true},
		"localhost":                {"http://localhost:8080", true},
		"127.0.0.1":                {"http://127.0.0.1:8080", true},
		"rfc1918 192.168":          {"http://192.168.1.100:8080", true},
		"rfc1918 10.x":             {"http://10.0.0.5:3000", true},
		"rfc1918 172.16":           {"http://172.20.0.2:8080", true},
		"evil domain":              {"https://evil.com", false},
		"evil subdomain localhost": {"http://localhost.evil.com", false},
		"evil subdomain 127":       {"http://127.0.0.1.evil.com", false},
		"evil subdomain 192.168":   {"http://192.168.evil.com", false},
		"invalid url":              {"://bad-url", false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := isAllowedOrigin(tc.origin)
			assert.Equal(t, tc.allow, got, "origin %q", tc.origin)
		})
	}
}

func TestEnforceBindAuth_AllowsConvenienceModeWithoutPassword(t *testing.T) {
	withAdminPassword(t, "")
	orig := authRequired
	authRequired = false
	t.Cleanup(func() { authRequired = orig })

	require.NoError(t, enforceBindAuth("127.0.0.1"))
	assert.False(t, authRequired)
	require.NoError(t, enforceBindAuth("localhost"))
	assert.False(t, authRequired)
	require.NoError(t, enforceBindAuth("0.0.0.0"))
	assert.False(t, authRequired)
}

func TestEnforceBindAuth_EnablesAuthWithPassword(t *testing.T) {
	withAdminPassword(t, "hunter2")
	orig := authRequired
	authRequired = false
	t.Cleanup(func() { authRequired = orig })

	require.NoError(t, enforceBindAuth("0.0.0.0"))
	assert.True(t, authRequired)
}

func TestHandleLogin_PasswordOnly(t *testing.T) {
	withSessionSecret(t, "the-secret")
	withAdminPassword(t, "hunter2")
	orig := authRequired
	authRequired = true
	t.Cleanup(func() { authRequired = orig })

	r := gin.New()
	r.POST("/login", handleLogin)

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("password succeeds with derived cookie", func(t *testing.T) {
		rec := post(`{"password":"hunter2"}`)
		assert.Equal(t, http.StatusOK, rec.Code)
		res := rec.Result()
		defer res.Body.Close()
		var session *http.Cookie
		for _, c := range res.Cookies() {
			if c.Name == "vocat_session" {
				session = c
			}
		}
		require.NotNil(t, session)
		assert.NotEqual(t, "the-secret", session.Value)
		assert.True(t, validSessionToken(session.Value))
	})

	t.Run("session secret is refused", func(t *testing.T) {
		rec := post(`{"password":"the-secret"}`)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		rec = post(`{"secret":"the-secret"}`)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("wrong password is refused", func(t *testing.T) {
		rec := post(`{"password":"nope"}`)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}

func TestResolveSessionSecret_RequireAuthNeedsPassword(t *testing.T) {
	t.Setenv("VOCAT_REQUIRE_AUTH", "true")
	t.Setenv("VOCAT_ADMIN_PASSWORD", "")
	t.Setenv("ADMIN_PASSWORD", "")
	t.Setenv("VOCAT_SESSION_SECRET", "")
	_, err := resolveSessionSecret(t.TempDir())
	require.Error(t, err)
}
