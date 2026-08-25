package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetDynamicModels_ProvidersAndFiltering(t *testing.T) {
	ctx := context.Background()
	providers := getDynamicModels(ctx, true)

	require.NotEmpty(t, providers)
	var ids []string
	for _, p := range providers {
		ids = append(ids, p.ID)
		assert.NotEmpty(t, p.Models, "provider %s should have models", p.ID)
		assert.NotEmpty(t, p.DefaultModel, "provider %s should have a default model", p.ID)

		// Check that legacy 1.5, 1.0, and Claude 3.x models are excluded
		for _, m := range p.Models {
			assert.NotContains(t, m.ID, "1.5", "legacy 1.5 model %s should be excluded", m.ID)
			assert.NotContains(t, m.ID, "1.0", "legacy 1.0 model %s should be excluded", m.ID)
			assert.NotContains(t, m.ID, "claude-3", "Claude 3.x model %s should be excluded", m.ID)
			assert.NotContains(t, m.ID, "claude-3-7", "Claude 3-7 model %s should be excluded", m.ID)
			assert.NotContains(t, m.ID, "claude-3-5", "Claude 3-5 model %s should be excluded", m.ID)
			assert.NotContains(t, m.ID, "anthropic.claude-3", "Claude 3.x model %s should be excluded", m.ID)
			assert.NotContains(t, m.ID, "embedding", "embedding model %s should be excluded", m.ID)
			assert.NotContains(t, m.ID, "tts", "tts model %s should be excluded", m.ID)
		}
	}

	assert.Contains(t, ids, "google-ai-studio")
	assert.Contains(t, ids, "vertex")
	assert.Contains(t, ids, "bedrock")
}
