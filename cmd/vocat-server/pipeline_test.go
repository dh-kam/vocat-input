package main

import (
	"path/filepath"
	"strings"
	"testing"

	"vocat-input/internal/engine"
)

// buildImagePaths must pass absolute paths through untouched and join relative names against the
// upload dir. The structuring providers os.ReadFile the result, so a regression here breaks every
// run that stored relative image names.
func TestBuildImagePaths(t *testing.T) {
	r := &engine.ConversionRun{
		Images: []string{
			"/var/lib/uploads/page1.jpg",          // absolute → untouched
			"run_123_abc-0-page2.jpg",             // relative → joined
			filepath.Join("..", "sneaky", "x.jpg"), // relative, traversing → still just joined
		},
	}
	got := buildImagePaths(r, "/data/uploads")

	want := []string{
		"/var/lib/uploads/page1.jpg",
		filepath.Join("/data/uploads", "run_123_abc-0-page2.jpg"),
		filepath.Join("/data/uploads", filepath.Join("..", "sneaky", "x.jpg")),
	}
	if len(got) != len(want) {
		t.Fatalf("got %d paths, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// ocrSnippetLog must flatten newlines and cap the preview so a multi-line raw OCR response cannot
// blow up the log stream, while still carrying its index for cross-referencing.
func TestOcrSnippetLog(t *testing.T) {
	out := ocrSnippetLog(3, "  line one\nline two\n\nline three  ")
	if !strings.Contains(out, "#3") {
		t.Errorf("expected the 1-based index in the snippet, got: %s", out)
	}
	if strings.Contains(out, "\n") {
		t.Errorf("newlines must be flattened to spaces, got: %s", out)
	}
	if !strings.Contains(out, "line one") || !strings.Contains(out, "line two") {
		t.Errorf("trimmed content dropped, got: %s", out)
	}

	long := strings.Repeat("x", 500)
	out = ocrSnippetLog(1, long)
	if !strings.HasSuffix(out, "...\"") {
		t.Errorf("long input must be truncated with '...', got tail: %q", out[len(out)-8:])
	}
	// 180-char cap + "..." before the closing quote.
	bodyStart := strings.Index(out, "\"")
	body := out[bodyStart+1 : len(out)-1] // strip wrapping quotes
	if !strings.HasSuffix(body, "...") || len(body) > 183 {
		t.Errorf("body not capped near 180 chars (len=%d): %q", len(body), body)
	}
}
