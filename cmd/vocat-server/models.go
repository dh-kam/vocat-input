package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"vocat-input/internal/engine"
)

var (
	modelsCacheMu  sync.RWMutex
	cachedModels   []ProviderOption
	modelsCachedAt time.Time
)

// getDynamicModels returns the available AI OCR and Structuring providers and their model lists.
// It queries live model catalogs (e.g. Google AI Studio ListModels API) when credentials are available,
// with an in-memory TTL cache to ensure low latency and high availability.
func getDynamicModels(ctx context.Context, forceRefresh bool) []ProviderOption {
	modelsCacheMu.RLock()
	if !forceRefresh && len(cachedModels) > 0 && time.Since(modelsCachedAt) < 5*time.Minute {
		res := cachedModels
		modelsCacheMu.RUnlock()
		return res
	}
	modelsCacheMu.RUnlock()

	modelsCacheMu.Lock()
	defer modelsCacheMu.Unlock()

	if !forceRefresh && len(cachedModels) > 0 && time.Since(modelsCachedAt) < 5*time.Minute {
		return cachedModels
	}

	googleStudioModels := fetchDynamicGoogleAIStudioModels(ctx)
	vertexModels := getCuratedVertexModels()
	bedrockModels := getCuratedBedrockModels()

	providers := []ProviderOption{
		{
			ID:           "google-ai-studio",
			Label:        "Google AI Studio",
			Desc:         "Google AI Studio Gemini API (Direct Key)",
			DefaultModel: "gemini-2.5-flash",
			Models:       googleStudioModels,
		},
		{
			ID:           "vertex",
			Label:        "GCP Vertex",
			Desc:         "Google Cloud Vertex AI (Gemini 2.5)",
			DefaultModel: "gemini-2.5-flash",
			Models:       vertexModels,
		},
		{
			ID:           "bedrock",
			Label:        "AWS Bedrock",
			Desc:         "Amazon Bedrock (Claude 4.6 & Nova)",
			DefaultModel: "us.anthropic.claude-sonnet-4-6",
			Models:       bedrockModels,
		},
	}

	cachedModels = providers
	modelsCachedAt = time.Now()
	return providers
}

func getCuratedVertexModels() []ModelOption {
	return []ModelOption{
		{ID: "gemini-2.5-flash", Label: "Gemini 2.5 Flash", Desc: "Fast, Multimodal & High Accuracy (Recommended)", Default: true},
		{ID: "gemini-2.5-pro", Label: "Gemini 2.5 Pro", Desc: "Deep Reasoning & Highest OCR Accuracy"},
		{ID: "gemini-3.7-flash", Label: "Gemini 3.7 Flash", Desc: "Next-gen Flagship Multimodal (Fast & Accurate)"},
		{ID: "gemini-3.1-pro-preview", Label: "Gemini 3.1 Pro Preview", Desc: "Advanced Next-gen Pro Reasoning"},
		{ID: "gemini-2.5-flash-lite", Label: "Gemini 2.5 Flash Lite", Desc: "Ultra-fast Lightweight"},
		{ID: "gemini-2.0-flash", Label: "Gemini 2.0 Flash", Desc: "High Throughput Multimodal"},
		{ID: "claude-sonnet-4-6", Label: "Claude 4.6 Sonnet", Desc: "State-of-the-art AI Multimodal"},
		{ID: "claude-opus-4-6", Label: "Claude 4.6 Opus", Desc: "Ultimate Reasoning & Deep Context"},
		{ID: "claude-4-5-fable", Label: "Claude 4.5 Fable", Desc: "Creative & Nuanced Context Analysis"},
		{ID: "claude-3-7-sonnet", Label: "Claude 3.7 Sonnet", Desc: "Hybrid Reasoning & Vision"},
	}
}

func getCuratedBedrockModels() []ModelOption {
	return []ModelOption{
		{ID: "us.anthropic.claude-sonnet-4-6", Label: "Claude 4.6 Sonnet", Desc: "State-of-the-art AI (Recommended)", Default: true},
		{ID: "us.anthropic.claude-opus-4-6", Label: "Claude 4.6 Opus", Desc: "Ultimate Reasoning & Deep Context"},
		{ID: "us.anthropic.claude-4-5-fable", Label: "Claude 4.5 Fable", Desc: "Creative & Nuanced Context Analysis"},
		{ID: "us.anthropic.claude-sonnet-4-5-20250929-v1:0", Label: "Claude 4.5 Sonnet", Desc: "High Performance Multimodal"},
		{ID: "us.anthropic.claude-3-7-sonnet-20250219-v1:0", Label: "Claude 3.7 Sonnet", Desc: "Hybrid Reasoning & Vision"},
		{ID: "us.anthropic.claude-3-5-sonnet-20241022-v2:0", Label: "Claude 3.5 Sonnet v2", Desc: "High Performance Multimodal"},
		{ID: "us.anthropic.claude-3-5-haiku-20241022-v1:0", Label: "Claude 3.5 Haiku", Desc: "Ultra-fast & Cost-Effective"},
		{ID: "amazon.nova-pro-v1:0", Label: "Nova Pro", Desc: "Higher Accuracy Reasoning"},
		{ID: "amazon.nova-lite-v1:0", Label: "Nova Lite", Desc: "Fast, Cost-Effective"},
	}
}

func getDefaultGoogleAIStudioModels() []ModelOption {
	return []ModelOption{
		{ID: "gemini-2.5-flash", Label: "Gemini 2.5 Flash", Desc: "Fast, Multimodal & Balanced (Recommended)", Default: true},
		{ID: "gemini-2.5-pro", Label: "Gemini 2.5 Pro", Desc: "Deep Reasoning & Highest Accuracy"},
		{ID: "gemini-3.7-flash", Label: "Gemini 3.7 Flash", Desc: "Next-gen Flagship Multimodal (Fast & Accurate)"},
		{ID: "gemini-3.1-pro-preview", Label: "Gemini 3.1 Pro Preview", Desc: "Advanced Next-gen Pro Reasoning"},
		{ID: "gemini-2.5-flash-lite", Label: "Gemini 2.5 Flash Lite", Desc: "Ultra-fast Lightweight"},
		{ID: "gemini-2.0-flash", Label: "Gemini 2.0 Flash", Desc: "High Throughput Multimodal"},
		{ID: "claude-sonnet-4-6", Label: "Claude 4.6 Sonnet", Desc: "State-of-the-art AI Multimodal"},
		{ID: "claude-opus-4-6", Label: "Claude 4.6 Opus", Desc: "Ultimate Reasoning & Deep Context"},
		{ID: "claude-4-5-fable", Label: "Claude 4.5 Fable", Desc: "Creative & Nuanced Context Analysis"},
		{ID: "claude-3-7-sonnet", Label: "Claude 3.7 Sonnet", Desc: "Hybrid Reasoning & Vision"},
	}
}

type googleListModelsResponse struct {
	Models []struct {
		Name                       string   `json:"name"`
		DisplayName                string   `json:"displayName"`
		Description                string   `json:"description"`
		SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
	} `json:"models"`
}

func fetchDynamicGoogleAIStudioModels(ctx context.Context) []ModelOption {
	apiKey := engine.LookupConfig("GOOGLE_AI_STUDIO_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "VERTEX_API_KEY", "VERTEX_AI_API_KEY")
	if apiKey == "" {
		return getDefaultGoogleAIStudioModels()
	}

	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models?key=%s", apiKey)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return getDefaultGoogleAIStudioModels()
	}

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return getDefaultGoogleAIStudioModels()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return getDefaultGoogleAIStudioModels()
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return getDefaultGoogleAIStudioModels()
	}

	var parsed googleListModelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil || len(parsed.Models) == 0 {
		return getDefaultGoogleAIStudioModels()
	}

	var results []ModelOption
	seen := make(map[string]bool)

	for _, m := range parsed.Models {
		// Only keep models supporting generateContent and containing "gemini"
		canGenerate := false
		for _, method := range m.SupportedGenerationMethods {
			if method == "generateContent" {
				canGenerate = true
				break
			}
		}
		if !canGenerate {
			continue
		}

		rawID := strings.TrimPrefix(m.Name, "models/")
		lowerID := strings.ToLower(rawID)
		if !strings.HasPrefix(lowerID, "gemini-") {
			continue
		}
		if strings.Contains(lowerID, "embedding") || strings.Contains(lowerID, "aqa") || strings.Contains(lowerID, "imagen") ||
			strings.Contains(lowerID, "1.5") || strings.Contains(lowerID, "1.0") || strings.Contains(lowerID, "bison") ||
			strings.Contains(lowerID, "learnlm") || strings.Contains(lowerID, "001") || strings.Contains(lowerID, "002") ||
			strings.Contains(lowerID, "tts") || strings.Contains(lowerID, "audio") || strings.Contains(lowerID, "clip") ||
			strings.Contains(lowerID, "lyria") || strings.Contains(lowerID, "robotics") || strings.Contains(lowerID, "-image") {
			continue
		}
		if seen[rawID] {
			continue
		}
		seen[rawID] = true

		label := m.DisplayName
		if label == "" {
			label = rawID
		}
		desc := m.Description
		if len(desc) > 80 {
			desc = desc[:77] + "..."
		}

		results = append(results, ModelOption{
			ID:      rawID,
			Label:   label,
			Desc:    desc,
			Default: rawID == "gemini-2.5-flash",
		})
	}

	if len(results) == 0 {
		return getDefaultGoogleAIStudioModels()
	}

	// Sort models with priority order
	modelPriority := map[string]int{
		"gemini-2.5-flash":       1,
		"gemini-2.5-pro":         2,
		"gemini-3.7-flash":       3,
		"gemini-3.1-pro-preview": 4,
		"gemini-3.1-pro":         4,
		"gemini-3.5-flash":       5,
		"gemini-2.5-flash-lite":  6,
		"gemini-2.0-flash":       7,
		"gemini-2.0-flash-lite":  8,
	}

	sort.SliceStable(results, func(i, j int) bool {
		p1, ok1 := modelPriority[results[i].ID]
		p2, ok2 := modelPriority[results[j].ID]
		if !ok1 {
			p1 = 100
		}
		if !ok2 {
			p2 = 100
		}
		if p1 != p2 {
			return p1 < p2
		}
		return results[i].ID < results[j].ID
	})

	// Also include latest Claude models (Sonnet 4.6, Opus 4.6, Fable 4.5, Sonnet 3.7) under Google AI Studio options
	results = append(results,
		ModelOption{ID: "claude-sonnet-4-6", Label: "Claude 4.6 Sonnet", Desc: "State-of-the-art AI Multimodal"},
		ModelOption{ID: "claude-opus-4-6", Label: "Claude 4.6 Opus", Desc: "Ultimate Reasoning & Deep Context"},
		ModelOption{ID: "claude-4-5-fable", Label: "Claude 4.5 Fable", Desc: "Creative & Nuanced Context Analysis"},
		ModelOption{ID: "claude-3-7-sonnet", Label: "Claude 3.7 Sonnet", Desc: "Hybrid Reasoning & Vision"},
	)

	return results
}
