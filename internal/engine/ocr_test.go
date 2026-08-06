package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubProvider struct {
	name string
	text string
	err  error
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) ProcessImage(context.Context, string) (string, error) {
	return s.text, s.err
}

// An unknown name used to resolve to the fallback chain, so a typo silently ran a different
// engine and every caller's error branch was unreachable.
func TestRegistryGet_UnknownNameIsAnError(t *testing.T) {
	r := NewProviderRegistry()

	for _, name := range []string{"bedrok", "", "vertexx", "gpt"} {
		p, err := r.Get(name)
		require.Error(t, err, "name %q should be refused", name)
		assert.Nil(t, p)
		assert.Contains(t, err.Error(), "available:", "the error should list the valid names")
	}
}

func TestRegistryGet_KnownNames(t *testing.T) {
	r := NewProviderRegistry()

	for _, name := range r.Names() {
		p, err := r.Get(name)
		require.NoError(t, err, "registered name %q should resolve", name)
		require.NotNil(t, p)
		assert.Equal(t, name, p.Name())
	}
	assert.Subset(t, r.Names(), []string{"vertex", "bedrock", "anthropic", "doublecheck", "fallback", "dummy"})
}

func TestRegistryGet_IsCaseAndSpaceInsensitive(t *testing.T) {
	r := NewProviderRegistry()
	for _, name := range []string{"VERTEX", " vertex ", "Vertex"} {
		p, err := r.Get(name)
		require.NoError(t, err, "name %q should resolve", name)
		assert.Equal(t, "vertex", p.Name())
	}
}

// A comma list with one bad entry used to drop it silently, so "vertex,bedrok" ran vertex alone
// with no indication the second engine was never consulted.
func TestRegistryGetMulti_RejectsUnknownEntries(t *testing.T) {
	r := NewProviderRegistry()

	p, err := r.Get("vertex,bedrok")
	require.Error(t, err)
	assert.Nil(t, p)
	assert.Contains(t, err.Error(), "bedrok")

	p, err = r.Get("vertex,bedrock")
	require.NoError(t, err)
	assert.Equal(t, "vertex+bedrock", p.Name())
}

// The dummy provider fabricates vocabulary, so it must be reachable only by explicit request.
func TestFallbackChain_DoesNotIncludeDummy(t *testing.T) {
	r := NewProviderRegistry()

	chain, err := r.Get("fallback")
	require.NoError(t, err)
	fc, ok := chain.(*FallbackChainOCRProvider)
	require.True(t, ok)

	for _, p := range fc.Providers {
		assert.NotEqual(t, "dummy", p.Name(), "the fallback chain must not reach the dummy provider")
	}

	// Still selectable on purpose.
	d, err := r.Get("dummy")
	require.NoError(t, err)
	assert.Equal(t, "dummy", d.Name())
}

// When every step fails the chain must report it, not invent a transcription.
func TestFallbackChain_ReportsFailureInsteadOfFabricating(t *testing.T) {
	chain := &FallbackChainOCRProvider{Providers: []OCRProvider{
		&stubProvider{name: "first", err: fmt.Errorf("404 not_found")},
		&stubProvider{name: "second", err: fmt.Errorf("no credentials")},
	}}

	text, err := chain.ProcessImage(context.Background(), "page.jpg")
	require.Error(t, err)
	assert.Empty(t, text)
	assert.Contains(t, err.Error(), "404 not_found")
	assert.Contains(t, err.Error(), "no credentials")
}

// An empty transcription is a failure, not a success with no words.
func TestFallbackChain_TreatsEmptyTextAsFailure(t *testing.T) {
	chain := &FallbackChainOCRProvider{Providers: []OCRProvider{
		&stubProvider{name: "blank", text: "   "},
		&stubProvider{name: "good", text: "1. apple 사과"},
	}}

	text, err := chain.ProcessImage(context.Background(), "page.jpg")
	require.NoError(t, err)
	assert.Equal(t, "1. apple 사과", text)
}

func TestFallbackChain_ReturnsFirstSuccess(t *testing.T) {
	chain := &FallbackChainOCRProvider{Providers: []OCRProvider{
		&stubProvider{name: "first", text: "from first"},
		&stubProvider{name: "second", text: "from second"},
	}}

	text, err := chain.ProcessImage(context.Background(), "page.jpg")
	require.NoError(t, err)
	assert.Equal(t, "from first", text)
}

// The pin was left on a model Anthropic retired on 2025-10-28, which made every request 404.
func TestAnthropicModel_DefaultIsNotTheRetiredPin(t *testing.T) {
	assert.NotEqual(t, "claude-3-5-sonnet-20241022", defaultAnthropicModel)
	assert.False(t, strings.HasPrefix(defaultAnthropicModel, "claude-3"),
		"the default should not be a Claude 3 family model")
}

func TestAnthropicModel_EnvOverride(t *testing.T) {
	assert.Equal(t, defaultAnthropicModel, anthropicModel())

	t.Setenv("ANTHROPIC_MODEL", "claude-opus-5")
	assert.Equal(t, "claude-opus-5", anthropicModel())
}
