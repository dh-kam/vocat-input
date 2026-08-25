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

// registerAssetRoutes mirrors the guarded group main wires up for /uploads and /outputs: the two
// hold user material, so they sit behind AuthMiddleware on the root engine rather than the open
// r.Static they used to be. This builds the same shape in isolation to prove the middleware fires
// on Static routes — a gin upgrade that silently dropped group handlers on Static would otherwise
// re-expose every uploaded image and generated sheet without any test noticing.
func registerAssetRoutes(t *testing.T, r *gin.Engine, dir string) {
	t.Helper()
	guarded := r.Group("")
	guarded.Use(AuthMiddleware())
	guarded.Static("/uploads", dir)
}

func TestStaticUploads_RequiresAuth(t *testing.T) {
	withSessionSecret(t, "the-secret")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "page.jpg"), []byte("not-a-real-jpeg"), 0o644))

	r := gin.New()
	registerAssetRoutes(t, r, dir)

	// No credential at all: the file exists on disk, but the route must still refuse.
	req := httptest.NewRequest(http.MethodGet, "/uploads/page.jpg", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "uploads must require a session")
	assert.NotContains(t, rec.Body.String(), "not-a-real-jpeg", "no file bytes should reach an unauthenticated caller")

	// A wrong cookie is rejected too, not just an absent one.
	req = httptest.NewRequest(http.MethodGet, "/uploads/page.jpg", nil)
	req.AddCookie(&http.Cookie{Name: "vocat_session", Value: "nope"})
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestStaticUploads_ServesWithSession(t *testing.T) {
	withSessionSecret(t, "the-secret")

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "page.jpg"), []byte("not-a-real-jpeg"), 0o644))

	r := gin.New()
	registerAssetRoutes(t, r, dir)

	tok, err := mintSessionToken()
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodGet, "/uploads/page.jpg", nil)
	req.AddCookie(&http.Cookie{Name: "vocat_session", Value: tok})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "not-a-real-jpeg", rec.Body.String())
}

// A missing file still 404s, and — because Static owns its own route — does so without hitting the
// SPA fallback in NoRoute. What matters here is that auth runs first: the path is never probed
// without a session.
func TestStaticUploads_MissingFileStillAuthenticated(t *testing.T) {
	withSessionSecret(t, "the-secret")

	r := gin.New()
	registerAssetRoutes(t, r, t.TempDir())

	req := httptest.NewRequest(http.MethodGet, "/uploads/missing.jpg", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "auth runs before the file is even looked up")
}
