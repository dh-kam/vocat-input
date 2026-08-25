package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var defaultHTTPClient = &http.Client{
	Timeout: 120 * time.Second,
}

type modelContextKey struct{}

// WithOCRModel returns a child context carrying the requested OCR model ID.
func WithOCRModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, modelContextKey{}, model)
}

// GetOCRModel retrieves the model ID stored in the context, if any.
func GetOCRModel(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(modelContextKey{}).(string); ok {
		return v
	}
	return ""
}

type OCRProvider interface {
	Name() string
	ProcessImage(ctx context.Context, imagePath string) (string, error)
}

type ProviderRegistry struct {
	providers map[string]OCRProvider
}

func NewProviderRegistry() *ProviderRegistry {
	r := &ProviderRegistry{
		providers: make(map[string]OCRProvider),
	}
	vertexProvider := &VertexAIOCRProvider{}
	geminiProvider := &GeminiOCRProvider{}
	gcpProvider := &GCPVisionOCRProvider{}
	dummyProvider := &DummyOCRProvider{}
	anthropicProvider := &AnthropicOCRProvider{}

	bedrockProvider := &BedrockOCRProvider{}

	doubleCheckProvider := &DoubleCheckOCRProvider{
		EngineA: vertexProvider,  // 1st OCR: Google Cloud Vertex AI (Gemini)
		EngineB: bedrockProvider, // 2nd OCR: AWS Bedrock (BEDROCK_MODEL, else the Nova candidates)
	}

	googleAIStudioProvider := &GoogleAIStudioOCRProvider{}

	r.Register(dummyProvider)
	r.Register(vertexProvider)
	r.Register(geminiProvider)
	r.Register(googleAIStudioProvider)
	r.Register(anthropicProvider)
	r.Register(gcpProvider)
	r.Register(bedrockProvider)
	r.Register(doubleCheckProvider)

	// Fallback Chain: Vertex AI -> Anthropic. DummyOCRProvider is deliberately not a step here;
	// it stays registered so it can be asked for by name for offline UI work, but it must never
	// be reached by accident because its output is fabricated.
	fallbackProvider := &FallbackChainOCRProvider{
		Providers: []OCRProvider{vertexProvider, anthropicProvider},
	}
	r.Register(fallbackProvider)

	return r
}

func (r *ProviderRegistry) Register(provider OCRProvider) {
	r.providers[provider.Name()] = provider
}

// Get resolves a provider name, or a comma-separated list of them.
//
// An unrecognised name is an error rather than a silent substitution. It used to return the
// fallback chain, so a typo like "bedrok" quietly ran a different engine with different
// credentials, cost and latency — and until the dummy step was removed from that chain, a typo
// combined with missing credentials produced fabricated vocabulary. Every caller already
// handles the error, so those branches were simply unreachable.
func (r *ProviderRegistry) Get(name string) (OCRProvider, error) {
	parts := strings.Split(name, ",")
	if len(parts) > 1 {
		return r.GetMulti(parts)
	}

	key := strings.ToLower(strings.TrimSpace(name))
	switch key {
	case "ai-studio", "google_ai_studio":
		key = "google-ai-studio"
	}
	p, ok := r.providers[key]
	if !ok {
		return nil, fmt.Errorf("unknown OCR provider %q; available: %s", name, strings.Join(r.Names(), ", "))
	}
	return p, nil
}

// Names lists the registered provider names in sorted order, for error messages and help text.
func (r *ProviderRegistry) Names() []string {
	names := make([]string, 0, len(r.providers))
	for n := range r.providers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (r *ProviderRegistry) GetMulti(names []string) (OCRProvider, error) {
	var list []OCRProvider
	var unknown []string
	for _, n := range names {
		n = strings.TrimSpace(n)
		if p, ok := r.providers[strings.ToLower(n)]; ok {
			list = append(list, p)
		} else {
			unknown = append(unknown, n)
		}
	}
	// Dropping an unrecognised entry silently meant "vertex,bedrok" ran vertex alone with no
	// indication the second engine was never consulted.
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown OCR provider(s) %s; available: %s",
			strings.Join(unknown, ", "), strings.Join(r.Names(), ", "))
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no OCR providers given")
	}
	if len(list) == 1 {
		return list[0], nil
	}
	return &MultiChainOCRProvider{Providers: list}, nil
}

// ----------------------------------------------------
// 0-0. Multi-Chain OCR Provider (Dynamic Multi Engine)
// ----------------------------------------------------
type MultiChainOCRProvider struct {
	Providers []OCRProvider
}

func (m *MultiChainOCRProvider) Name() string {
	var names []string
	for _, p := range m.Providers {
		names = append(names, p.Name())
	}
	return strings.Join(names, "+")
}

func (m *MultiChainOCRProvider) ProcessImage(ctx context.Context, imagePath string) (string, error) {
	var results []string
	successCount := 0
	for i, provider := range m.Providers {
		log.Printf("[MultiChain OCR] Executing Step %d/%d: %s...", i+1, len(m.Providers), provider.Name())
		text, err := provider.ProcessImage(ctx, imagePath)
		if err != nil {
			log.Printf("[MultiChain OCR Warning] Provider %s failed: %v", provider.Name(), err)
			results = append(results, fmt.Sprintf("=== OCR PROVIDER #%d (%s: FAILED) ===\nError: %v", i+1, provider.Name(), err))
		} else {
			log.Printf("[MultiChain OCR] Provider %s completed successfully.", provider.Name())
			results = append(results, fmt.Sprintf("=== OCR PROVIDER #%d (%s) ===\n%s", i+1, provider.Name(), text))
			successCount++
		}
	}
	if successCount == 0 && len(m.Providers) > 0 {
		return "", fmt.Errorf("all %d OCR providers in chain failed", len(m.Providers))
	}
	return strings.Join(results, "\n\n"), nil
}

// ----------------------------------------------------
// 0-1. Double Check OCR Provider (Gemini + Anthropic Cross-Verification)
// ----------------------------------------------------
type DoubleCheckOCRProvider struct {
	EngineA OCRProvider // Primary: Vertex AI Gemini Vision
	EngineB OCRProvider // Secondary: Anthropic Claude Vision
}

func (d *DoubleCheckOCRProvider) Name() string { return "doublecheck" }

func (d *DoubleCheckOCRProvider) ProcessImage(ctx context.Context, imagePath string) (string, error) {
	log.Printf("[DoubleCheck OCR] Running Engine A (%s)...", d.EngineA.Name())
	textA, errA := d.EngineA.ProcessImage(ctx, imagePath)

	log.Printf("[DoubleCheck OCR] Running Engine B (%s)...", d.EngineB.Name())
	textB, errB := d.EngineB.ProcessImage(ctx, imagePath)

	if errA != nil && errB != nil {
		return "", fmt.Errorf("both OCR engines failed: EngineA (%v), EngineB (%v)", errA, errB)
	}

	if errA == nil && errB == nil {
		return fmt.Sprintf("=== OCR ENGINE A (%s) ===\n%s\n\n=== OCR ENGINE B (%s) ===\n%s", d.EngineA.Name(), textA, d.EngineB.Name(), textB), nil
	}

	if errA == nil {
		return textA, nil
	}
	return textB, nil
}

// ----------------------------------------------------
// 0. Fallback Chain Provider (Vertex AI -> Anthropic)
// ----------------------------------------------------
// FallbackChainOCRProvider tries each provider in order and returns the first non-empty
// transcription.
//
// When every step fails it reports their errors. It used to end at DummyOCRProvider, whose five
// hardcoded vocabulary entries were returned with a nil error, so a run with no working
// credentials "succeeded" and shipped invented words through structuring, DOC export and
// Telegram delivery with nothing surfaced to the user.
type FallbackChainOCRProvider struct {
	Providers []OCRProvider
}

func (f *FallbackChainOCRProvider) Name() string { return "fallback" }

func (f *FallbackChainOCRProvider) ProcessImage(ctx context.Context, imagePath string) (string, error) {
	var failures []error

	for i, p := range f.Providers {
		log.Printf("[OCR Pipeline] Step %d: trying %s...", i+1, p.Name())
		text, err := p.ProcessImage(ctx, imagePath)
		if err == nil && strings.TrimSpace(text) != "" {
			log.Printf("[OCR Pipeline] Step %d success: %s completed.", i+1, p.Name())
			return text, nil
		}
		if err == nil {
			err = fmt.Errorf("returned an empty transcription")
		}
		log.Printf("[OCR Pipeline Warning] %s failed: %v", p.Name(), err)
		failures = append(failures, fmt.Errorf("%s: %w", p.Name(), err))
	}

	return "", fmt.Errorf("every provider in the fallback chain failed: %w", errors.Join(failures...))
}

// getGoogleOCRAuth resolves API key or OAuth token for Google Gemini/Vertex OCR providers.
func getGoogleOCRAuth(ctx context.Context) (token, projectID, location string, isAPIKey bool) {
	// 1. Check API Key environment variables or .env file
	if v := LookupConfig("VERTEX_API_KEY", "VERTEX_AI_API_KEY", "GEMINI_API_KEY", "GOOGLE_AI_STUDIO_API_KEY"); v != "" {
		return v, "c0de1ab-dev-494714", "us-central1", true
	}

	// 2. OAuth token fallback (gcloud auth print-access-token)
	token = LookupConfig("GCLOUD_ACCESS_TOKEN")
	if token == "" {
		cmd := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token")
		out, err := cmd.Output()
		if err == nil {
			token = strings.TrimSpace(string(out))
		}
	}

	projectID = LookupConfig("GCP_PROJECT_ID")
	if projectID == "" {
		cmd := exec.CommandContext(ctx, "gcloud", "config", "get-value", "project")
		out, err := cmd.Output()
		if err == nil {
			projectID = strings.TrimSpace(string(out))
		}
	}
	if projectID == "" {
		projectID = "c0de1ab-dev-494714"
	}

	location = LookupConfig("GCP_LOCATION")
	if location == "" {
		location = "us-central1"
	}

	if token != "" {
		return token, projectID, location, false
	}

	return "", "", "", false
}

// ----------------------------------------------------
// 1. Google Cloud Vertex AI Vision OCR Provider
// ----------------------------------------------------
type VertexAIOCRProvider struct{}

func (v *VertexAIOCRProvider) Name() string { return "vertex" }

func (v *VertexAIOCRProvider) ProcessImage(ctx context.Context, imagePath string) (string, error) {
	token, projectID, location, isAPIKey := getGoogleOCRAuth(ctx)
	if token == "" {
		return "", fmt.Errorf("Google OCR requires API key (VERTEX_API_KEY/GEMINI_API_KEY) or OAuth access token (gcloud auth login)")
	}

	imgBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("read image file: %w", err)
	}

	mimeType := "image/jpeg"
	ext := strings.ToLower(filepath.Ext(imagePath))
	if ext == ".png" {
		mimeType = "image/png"
	} else if ext == ".webp" {
		mimeType = "image/webp"
	}

	base64Data := base64.StdEncoding.EncodeToString(imgBytes)

	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{
						"inline_data": map[string]string{
							"mime_type": mimeType,
							"data":      base64Data,
						},
					},
					{
						"text": `Transcribe all text from this image line by line with absolute accuracy.

CRITICAL RULES FOR TRANSCRIPTION:
1. NO CONVERSATIONAL PREAMBLES: NEVER include lines like "Here is the transcription...", "Here are the words...", or any introductory/closing conversational text.
2. NO MARKDOWN DECORATIONS: Do NOT add markdown bold (**word**), bullet points (*), or italics. Transcribe plain text only.
3. PRESERVE ALL ITEM NUMBERS: Keep all row numbers (e.g. 51, 52, 53, 76, 77) exactly as written at the start of lines.
4. EXACT TEXT ONLY: Transcribe the exact text visible on the page line by line.`,
					},
				},
			},
		},
	}

	modelName := GetOCRModel(ctx)
	if modelName == "" {
		modelName = LookupConfig("VERTEX_MODEL")
	}
	mLower := strings.ToLower(modelName)
	if strings.Contains(mLower, "claude") || strings.Contains(mLower, "opus") || strings.Contains(mLower, "sonnet") || strings.Contains(mLower, "haiku") {
		b := &BedrockOCRProvider{}
		return b.ProcessImage(ctx, imagePath)
	}
	if modelName == "" {
		modelName = "gemini-2.5-flash"
	}

	jsonPayload, _ := json.Marshal(payload)

	var endpoint string
	if isAPIKey {
		endpoint = fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent?key=%s", location, projectID, location, modelName, token)
	} else {
		endpoint = fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent", location, projectID, location, modelName)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if !isAPIKey {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return "", RedactedError("vertex api request failed: %s", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vertex api error (status %d): %s", resp.StatusCode, string(body))
	}

	var resStruct struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(body, &resStruct); err != nil {
		return "", fmt.Errorf("unmarshal vertex response: %w", err)
	}

	if len(resStruct.Candidates) > 0 && len(resStruct.Candidates[0].Content.Parts) > 0 {
		return resStruct.Candidates[0].Content.Parts[0].Text, nil
	}
	return "", fmt.Errorf("empty response from Vertex AI API")
}

// getGoogleAIStudioAuth resolves the API key for Google AI Studio / Gemini Developer API.
func getGoogleAIStudioAuth() string {
	return LookupConfig("GOOGLE_AI_STUDIO_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "VERTEX_API_KEY", "VERTEX_AI_API_KEY")
}

// ----------------------------------------------------
// 1. Google AI Studio (Gemini Developer API) OCR Provider
// ----------------------------------------------------
type GoogleAIStudioOCRProvider struct{}

func (g *GoogleAIStudioOCRProvider) Name() string { return "google-ai-studio" }

func (g *GoogleAIStudioOCRProvider) ProcessImage(ctx context.Context, imagePath string) (string, error) {
	apiKey := getGoogleAIStudioAuth()
	if apiKey == "" {
		return "", fmt.Errorf("Google AI Studio requires an API key (set GOOGLE_AI_STUDIO_API_KEY or GEMINI_API_KEY in .env)")
	}

	imgBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("read image file: %w", err)
	}

	mimeType := "image/jpeg"
	ext := strings.ToLower(filepath.Ext(imagePath))
	if ext == ".png" {
		mimeType = "image/png"
	} else if ext == ".webp" {
		mimeType = "image/webp"
	}

	base64Data := base64.StdEncoding.EncodeToString(imgBytes)

	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{
						"inline_data": map[string]string{
							"mime_type": mimeType,
							"data":      base64Data,
						},
					},
					{
						"text": `Transcribe all text from this image line by line with absolute accuracy.

CRITICAL RULES FOR TRANSCRIPTION:
1. NO CONVERSATIONAL PREAMBLES: NEVER include lines like "Here is the transcription...", "Here are the words...", or any introductory/closing conversational text.
2. NO MARKDOWN DECORATIONS: Do NOT add markdown bold (**word**), bullet points (*), or italics. Transcribe plain text only.
3. PRESERVE ALL ITEM NUMBERS: Keep all row numbers (e.g. 51, 52, 53, 76, 77) exactly as written at the start of lines.
4. EXACT TEXT ONLY: Transcribe the exact text visible on the page line by line.`,
					},
				},
			},
		},
	}

	modelName := GetOCRModel(ctx)
	if modelName == "" {
		modelName = LookupConfig("GEMINI_MODEL", "GOOGLE_AI_STUDIO_MODEL")
	}
	mLower := strings.ToLower(modelName)
	if strings.Contains(mLower, "claude") || strings.Contains(mLower, "opus") || strings.Contains(mLower, "sonnet") || strings.Contains(mLower, "haiku") {
		b := &BedrockOCRProvider{}
		return b.ProcessImage(ctx, imagePath)
	}
	if modelName == "" {
		modelName = "gemini-2.5-flash"
	}

	jsonPayload, _ := json.Marshal(payload)

	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", modelName, apiKey)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return "", RedactedError("Google AI Studio API request failed: %s", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Google AI Studio API error (status %d): %s", resp.StatusCode, string(body))
	}

	var resStruct struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(body, &resStruct); err != nil {
		return "", fmt.Errorf("unmarshal Google AI Studio response: %w", err)
	}

	if len(resStruct.Candidates) > 0 && len(resStruct.Candidates[0].Content.Parts) > 0 {
		return resStruct.Candidates[0].Content.Parts[0].Text, nil
	}
	return "", fmt.Errorf("empty response from Google AI Studio API")
}

// ----------------------------------------------------
// 1. Google AI Studio (Gemini Vision) Legacy Alias OCR Provider
// ----------------------------------------------------
type GeminiOCRProvider struct{}

func (g *GeminiOCRProvider) Name() string { return "gemini" }

func (g *GeminiOCRProvider) ProcessImage(ctx context.Context, imagePath string) (string, error) {
	p := &GoogleAIStudioOCRProvider{}
	return p.ProcessImage(ctx, imagePath)
}

// ----------------------------------------------------
// 1. Dummy OCR Provider (Local Engine)
// ----------------------------------------------------
type DummyOCRProvider struct{}

func (d *DummyOCRProvider) Name() string { return "dummy" }

func (d *DummyOCRProvider) ProcessImage(ctx context.Context, imagePath string) (string, error) {
	filename := filepath.Base(imagePath)
	return fmt.Sprintf(`1. critical [adjective] 위독한, 중요한
2. assign [verb] 맡기다, 배정하다
3. emphasize [verb] 강조하다
4. reluctant [adjective] 꺼리는, 마지못해 하는
5. perspective [noun] 관점, 시각 (Extracted from %s)`, filename), nil
}

// ----------------------------------------------------
// 2-1. AWS Bedrock Vision OCR Provider (Amazon Nova Vision Engine)
// ----------------------------------------------------
type BedrockOCRProvider struct{}

func (b *BedrockOCRProvider) Name() string { return "bedrock" }

func (b *BedrockOCRProvider) ProcessImage(ctx context.Context, imagePath string) (string, error) {
	bearerToken := LookupConfig("AWS_BEARER_TOKEN_BEDROCK")
	if bearerToken == "" {
		return "", fmt.Errorf("AWS_BEARER_TOKEN_BEDROCK not set in environment or .env")
	}

	imgBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("read image file: %w", err)
	}

	imgFormat := "jpeg"
	ext := strings.ToLower(filepath.Ext(imagePath))
	if ext == ".png" {
		imgFormat = "png"
	} else if ext == ".webp" {
		imgFormat = "webp"
	}

	base64Data := base64.StdEncoding.EncodeToString(imgBytes)

	// AWS Bedrock Amazon Nova Vision Payload Specification
	payload := map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"image": map[string]interface{}{
							"format": imgFormat,
							"source": map[string]string{
								"bytes": base64Data,
							},
						},
					},
					{
						"text": `You are an expert OCR transcription engine for vocabulary study materials.

Transcribe all text from this image accurately while preserving structural layout:
1. INDEX NUMBERS: You MUST capture all entry index numbers (e.g. 51, 52, 53, 66, 67, 81, 82, 83) at the beginning of each headword line. NEVER omit these numbers.
2. HEADWORDS & MEANINGS: Transcribe each main entry as "[number] [English word] ([part of speech]) [Korean meaning]".
3. ANNOTATIONS & EXAMPLES: Clearly separate example phrases, example sentences, synonyms (=), antonyms (≠), and derived words (->) from main headwords.
4. COMPLETE COVERAGE: Transcribe every single text element on the page without skipping numbers or words.`,
					},
				},
			},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal bedrock payload: %w", err)
	}

	region := LookupConfig("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	candidateModels := []string{
		"amazon.nova-lite-v1:0",
		"amazon.nova-pro-v1:0",
		"us.amazon.nova-lite-v1:0",
	}
	ctxModel := GetOCRModel(ctx)
	if ctxModel != "" && !strings.Contains(strings.ToLower(ctxModel), "gemini") {
		candidateModels = append([]string{ctxModel}, candidateModels...)
	} else if envModel := LookupConfig("BEDROCK_MODEL"); envModel != "" && !strings.Contains(strings.ToLower(envModel), "gemini") {
		candidateModels = append([]string{envModel}, candidateModels...)
	}

	var lastErr error
	for _, modelID := range candidateModels {
		endpoint := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/invoke", region, modelID)
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonPayload))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bearerToken))

		resp, err := defaultHTTPClient.Do(req)
		if err != nil {
			lastErr = RedactedError("bedrock request failed for "+modelID+": %s", err)
			continue
		}

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("bedrock api error for %s (status %d): %s", modelID, resp.StatusCode, string(body))
			continue
		}

		var resStruct struct {
			Output struct {
				Message struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			} `json:"output"`
		}

		if err := json.Unmarshal(body, &resStruct); err == nil && len(resStruct.Output.Message.Content) > 0 {
			text := resStruct.Output.Message.Content[0].Text
			if strings.TrimSpace(text) != "" {
				log.Printf("[Bedrock OCR Success] AWS Bedrock Nova Model '%s' successfully processed image '%s'", modelID, filepath.Base(imagePath))
				return text, nil
			}
		}
	}

	return "", fmt.Errorf("AWS Bedrock OCR execution failed across all Nova Vision models: %v", lastErr)
}

// ----------------------------------------------------
// 2. Anthropic Claude Vision OCR Provider
// ----------------------------------------------------
type AnthropicOCRProvider struct{}

func (a *AnthropicOCRProvider) Name() string { return "anthropic" }

// defaultAnthropicModel is the model the direct Anthropic API calls use unless ANTHROPIC_MODEL
// overrides it. It was pinned to claude-3-5-sonnet-20241022, which Anthropic retired on
// 2025-10-28: every request returned 404 not_found, so selecting the anthropic provider always
// failed and it could never rescue a Vertex failure in the fallback chain. The sibling Vertex
// and Bedrock providers have always been env-overridable; this one was not, which is why the
// stale pin went unnoticed.
const defaultAnthropicModel = "claude-sonnet-5"

func anthropicModel() string {
	if m := LookupConfig("ANTHROPIC_MODEL"); m != "" {
		return m
	}
	return defaultAnthropicModel
}

func (a *AnthropicOCRProvider) ProcessImage(ctx context.Context, imagePath string) (string, error) {
	apiKey := LookupConfig("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	imgBytes, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("read image file: %w", err)
	}

	mimeType := "image/jpeg"
	if strings.HasSuffix(strings.ToLower(imagePath), ".png") {
		mimeType = "image/png"
	} else if strings.HasSuffix(strings.ToLower(imagePath), ".webp") {
		mimeType = "image/webp"
	}

	base64Data := base64.StdEncoding.EncodeToString(imgBytes)

	payload := map[string]interface{}{
		"model":      anthropicModel(),
		"max_tokens": 2048,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "image",
						"source": map[string]string{
							"type":       "base64",
							"media_type": mimeType,
							"data":       base64Data,
						},
					},
					{
						"type": "text",
						"text": "Please transcribe all English vocabulary, part of speech, and Korean meanings from this image.",
					},
				},
			},
		},
	}

	jsonPayload, _ := json.Marshal(payload)
	baseURL := LookupConfig("ANTHROPIC_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	endpoint := fmt.Sprintf("%s/v1/messages", strings.TrimSuffix(baseURL, "/"))

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return "", RedactedError("anthropic api request failed: %s", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic api error (status %d): %s", resp.StatusCode, string(body))
	}

	var resStruct struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &resStruct); err != nil {
		return "", err
	}

	if len(resStruct.Content) > 0 {
		return resStruct.Content[0].Text, nil
	}
	return "", fmt.Errorf("empty text response from anthropic OCR")
}

// ----------------------------------------------------
// 3. GCP Vision OCR Provider
// ----------------------------------------------------
type GCPVisionOCRProvider struct{}

func (g *GCPVisionOCRProvider) Name() string { return "gcp" }

func (g *GCPVisionOCRProvider) ProcessImage(ctx context.Context, imagePath string) (string, error) {
	apiKey := LookupConfig("GCP_VISION_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("GCP_VISION_API_KEY not set")
	}
	return "", fmt.Errorf("GCP Vision API key configured but pending SDK execution")
}
