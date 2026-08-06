package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The traversal that reached os.ReadFile in the OCR providers and os.Remove in the delete
// handler. filepath.Join collapses "..", so these names used to resolve to real paths outside
// the storage tree.
func TestResolveInDir_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()

	rejected := []string{
		"../../.env",
		"../../../../etc/passwd",
		"..",
		".",
		"",
		"sub/child.jpg",
		"./a.jpg",
		"a/../b.jpg",
		filepath.Join("..", "outside.jpg"),
	}

	for _, name := range rejected {
		t.Run("reject "+name, func(t *testing.T) {
			got, err := resolveInDir(dir, name)
			assert.Error(t, err, "name %q should be refused, got %q", name, got)
			assert.Empty(t, got)
		})
	}
}

// Absolute names do not escape via filepath.Join — it nests them — but they are still not the
// flat names this server stores, so they must be refused rather than silently nested.
func TestResolveInDir_RejectsAbsoluteNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"/etc/hosts", "/tmp/x.jpg"} {
		_, err := resolveInDir(dir, name)
		assert.Error(t, err, "absolute name %q should be refused", name)
	}
}

func TestResolveInDir_AcceptsPlainFileNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"photo.jpg", "photo_2026-08-02_11-36-35.jpg", "a b.png", "..hidden.jpg", "run_1.doc"} {
		got, err := resolveInDir(dir, name)
		require.NoError(t, err, "name %q should be accepted", name)
		assert.Equal(t, filepath.Join(dir, name), got)
		assert.True(t, containedIn(dir, got))
	}
}

func TestContainedIn(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()

	assert.True(t, containedIn(dir, filepath.Join(dir, "a.jpg")))
	assert.True(t, containedIn(dir, filepath.Join(dir, "nested", "a.jpg")))
	assert.True(t, containedIn(dir, dir), "the directory itself is contained")

	assert.False(t, containedIn(dir, filepath.Join(outside, "a.jpg")))
	assert.False(t, containedIn(dir, "/etc/passwd"))
	assert.False(t, containedIn(dir, filepath.Dir(dir)), "the parent is not contained")

	// A sibling directory whose name merely starts with dir's name must not pass.
	assert.False(t, containedIn(dir, dir+"-evil/a.jpg"))
}

// containedIn compares resolved paths, so a relative stored path is judged against the same
// working directory the server runs from rather than being treated as unknown.
func TestContainedIn_RelativeStoredPath(t *testing.T) {
	orig, err := os.Getwd()
	require.NoError(t, err)
	base := t.TempDir()
	require.NoError(t, os.Chdir(base))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	uploads := filepath.Join(base, "storage", "uploads")
	require.NoError(t, os.MkdirAll(uploads, 0o755))

	assert.True(t, containedIn(uploads, filepath.Join("storage", "uploads", "a.jpg")))
	assert.False(t, containedIn(uploads, filepath.Join("storage", "outputs", "a.json")))
}
