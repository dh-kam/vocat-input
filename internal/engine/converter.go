package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"
)

var defaultPosMapping = map[string]string{
	"adjective":    "형",
	"adj":          "형",
	"noun":         "명",
	"n":            "명",
	"verb":         "동",
	"v":            "동",
	"adverb":       "부",
	"adv":          "부",
	"preposition":  "전",
	"prep":         "전",
	"conjunction":  "접",
	"conj":         "접",
	"article":      "관",
	"interjection": "감",
}

func NormalizePOS(rawPos string) string {
	clean := strings.Trim(strings.ToLower(rawPos), " []().")
	if mapped, ok := defaultPosMapping[clean]; ok {
		return mapped
	}
	if clean != "" {
		return string([]rune(clean)[0])
	}
	return "명"
}

func ConvertOCRToVocatJSON(ctx context.Context, mergedText string, preserveOrder bool, imagePaths []string, ocrProvider string, ocrModel string) ([]WordItem, error) {
	targetBBoxScale := GetModelBBoxScale(ocrProvider, ocrModel)

	fmt.Printf("[AI Structuring Engine] Launching with Provider: '%s', Model: '%s' (BBoxScale: %d)\n", ocrProvider, ocrModel, targetBBoxScale)

	// Stage 1: Multimodal Vision Format Analysis
	var formatInstructions string
	if len(imagePaths) > 0 {
		var samplePaths []string
		if len(imagePaths) > 2 {
			samplePaths = imagePaths[:2]
		} else {
			samplePaths = imagePaths
		}
		inst, err := analyzeFormatWithImage(ctx, samplePaths, mergedText)
		if err == nil && inst != "" {
			formatInstructions = inst
			fmt.Printf("[AI Structuring Stage 1] Format Analysis Completed (%d chars instructions)\n", len(inst))
		}
	}

	// Stage 2: Extract structured JSON using selected Provider & Model
	if ocrProvider == "bedrock" {
		fmt.Printf("[AI Structuring Stage 2] Calling AWS Bedrock Model '%s'...\n", ocrModel)
		words, err := callBedrockForJSON(ctx, ocrModel, mergedText, formatInstructions, imagePaths)
		if err == nil && len(words) > 0 {
			fmt.Printf("[AI Structuring Stage 2 Success] Extracted %d structured words with Bedrock '%s'\n", len(words), ocrModel)
			return cleanWordItems(words, targetBBoxScale), nil
		}
		fmt.Printf("[AI Structuring Warning] Bedrock Model '%s' call failed or empty (%v). Trying Vertex fallback...\n", ocrModel, err)
	}

	// Default / Vertex AI Engine
	fmt.Printf("[AI Structuring Stage 2] Calling GCP Vertex AI Model '%s'...\n", ocrModel)
	if strings.Contains(ocrModel, "claude") {
		words, err := callBedrockForJSON(ctx, "us.anthropic.claude-sonnet-4-6", mergedText, formatInstructions, imagePaths)
		if err == nil && len(words) > 0 {
			fmt.Printf("[AI Structuring Stage 2 Success] Extracted %d structured words with Vertex/Anthropic '%s'\n", len(words), ocrModel)
			return cleanWordItems(words, targetBBoxScale), nil
		}
	}
	words, err := callVertexForJSON(ctx, ocrModel, mergedText, formatInstructions, imagePaths)
	if err == nil && len(words) > 0 {
		fmt.Printf("[AI Structuring Stage 2 Success] Extracted %d structured words with Vertex '%s'\n", len(words), ocrModel)
		return cleanWordItems(words, targetBBoxScale), nil
	}

	// 3. Fallback: Anthropic Direct API
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey != "" {
		words, err := callClaudeForJSON(ctx, apiKey, mergedText)
		if err == nil && len(words) > 0 {
			return cleanWordItems(words, targetBBoxScale), nil
		}
	}

	// 4. Regex Fallback Parser
	return cleanWordItems(parseOCRTextFallback(mergedText, preserveOrder), targetBBoxScale), nil
}

// analyzeFormatWithImage sends up to 2 sample images to Gemini multimodal to understand
// the vocabulary material format and generate tailored extraction instructions.
func analyzeFormatWithImage(ctx context.Context, samplePaths []string, ocrSample string) (string, error) {
	token, projectID, location, isAPIKey := getVertexCredentials(ctx)
	if token == "" {
		return "", fmt.Errorf("no credentials for format analysis")
	}

	// Truncate OCR sample to first 2000 chars for the analysis prompt
	sample := ocrSample
	if len(sample) > 2000 {
		sample = sample[:2000] + "\n... (truncated)"
	}

	analysisPrompt := fmt.Sprintf(`You are a vocabulary material format analyst.

I'm showing you %d image(s) of a Korean-English vocabulary study material, along with a sample of OCR text extracted from it.

YOUR TASK: Analyze the image(s) to understand the EXACT layout and format of this vocabulary material, then write specific extraction instructions.

IMAGE ANALYSIS — answer these questions:
1. How are individual vocabulary entries visually structured? (numbered? bulleted? table rows? cards?)
2. What info is present per entry? (headword, POS, Korean meaning, synonyms, antonyms, derived forms, example sentences, translations?)
3. How is each piece of information visually distinguished? (by position? markers like "=", "≠", "-"? font size? color? indentation?)
4. What is the numbering scheme? (sequential? grouped? per-page?)
5. What content is supplementary and MUST BE EXCLUDED from becoming a headword entry? (e.g., example phrases like 'germ killer', example sentences, derived words, synonyms).

Then write EXTRACTION INSTRUCTIONS — a clear, specific prompt section that tells an AI:
- Exactly how to identify a single main headword entry in this specific format
- STRICT EXCLUSION: Explicitly list what to SKIP (example phrases/collocations e.g. 'germ killer', example sentences, Korean translations of sentences, derived forms)
- How to extract the main word, POS, and Korean meaning for each entry
- STRICT PRESERVATION: Require keeping the EXACT printed Korean definitions from the image without replacing them with similar/synonym words (e.g., preserve "조산사", NEVER replace with "산부인과 의사")
- How to preserve original numbering

OUTPUT FORMAT: Return ONLY the extraction instructions text (no JSON, no code block, no preamble). 
Write it as if you're adding instructions to a prompt. Be specific to THIS material's format.

OCR TEXT SAMPLE:
%s`, len(samplePaths), sample)

	// Build parts: image(s) first, then the text prompt
	var parts []map[string]interface{}
	for _, imgPath := range samplePaths {
		imgData, err := os.ReadFile(imgPath)
		if err != nil {
			fmt.Printf("[WARN] skip image %s: %v\n", imgPath, err)
			continue
		}
		mimeType := "image/jpeg"
		if strings.ToLower(filepath.Ext(imgPath)) == ".png" {
			mimeType = "image/png"
		}
		parts = append(parts, map[string]interface{}{
			"inline_data": map[string]interface{}{
				"mime_type": mimeType,
				"data":      encodeBase64(imgData),
			},
		})
	}
	parts = append(parts, map[string]interface{}{"text": analysisPrompt})

	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{"role": "user", "parts": parts},
		},
		"generationConfig": map[string]interface{}{
			"temperature": 0.2,
			"maxOutputTokens": 2048,
		},
	}

	jsonPayload, _ := json.Marshal(payload)
	modelName := os.Getenv("VERTEX_MODEL")
	if modelName == "" {
		modelName = "gemini-2.5-flash"
	}

	endpoint := buildVertexEndpoint(modelName, location, projectID, token, isAPIKey)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthHeader(req, token, isAPIKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("format analysis request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("format analysis error (status %d): %s", resp.StatusCode, string(body))
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
		return "", err
	}
	if len(resStruct.Candidates) == 0 || len(resStruct.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from format analysis")
	}

	instructions := strings.TrimSpace(resStruct.Candidates[0].Content.Parts[0].Text)
	fmt.Printf("[INFO] Stage 1 Format Analysis complete (%d chars instructions)\n", len(instructions))
	return instructions, nil
}

// getVertexCredentials resolves credentials for Vertex/Gemini API calls.
// Returns isAPIKey=true when using an API key (generativelanguage endpoint),
// isAPIKey=false when using OAuth token (aiplatform endpoint).
func getVertexCredentials(ctx context.Context) (token, projectID, location string, isAPIKey bool) {
	// 1. Check for API key first (Gemini API / AI Studio style)
	token = os.Getenv("VERTEX_AI_API_KEY")
	if token == "" {
		token = os.Getenv("VERTEX_API_KEY")
	}
	if token != "" {
		isAPIKey = true
		// API key doesn't need projectID/location but set defaults anyway
		projectID = os.Getenv("GCP_PROJECT_ID")
		if projectID == "" {
			projectID = "c0de1ab-dev-494714"
		}
		location = os.Getenv("GCP_LOCATION")
		if location == "" {
			location = "us-central1"
		}
		return
	}

	// 2. OAuth token flow (Vertex AI)
	token = os.Getenv("GCLOUD_ACCESS_TOKEN")
	if token == "" {
		cmd := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token")
		out, err := cmd.Output()
		if err == nil {
			token = strings.TrimSpace(string(out))
		}
	}

	projectID = os.Getenv("GCP_PROJECT_ID")
	if projectID == "" {
		cmd := exec.CommandContext(ctx, "gcloud", "config", "get-value", "project")
		out, err := cmd.Output()
		if err == nil {
			projectID = strings.TrimSpace(string(out))
		}
	}
	if projectID == "" || projectID == "c0de1ab-dev" {
		projectID = "c0de1ab-dev-494714"
	}

	location = os.Getenv("GCP_LOCATION")
	if location == "" {
		location = "us-central1"
	}
	return
}

// buildVertexEndpoint returns the correct Vertex AI endpoint URL and sets auth on the request.
func buildVertexEndpoint(modelName, location, projectID, token string, isAPIKey bool) string {
	if isAPIKey {
		// Vertex AI API with API key parameter on aiplatform.googleapis.com
		return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent?key=%s",
			location, projectID, location, modelName, token)
	}
	// Vertex AI API with OAuth Bearer token
	return fmt.Sprintf("https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		location, projectID, location, modelName)
}

func setAuthHeader(req *http.Request, token string, isAPIKey bool) {
	if !isAPIKey {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}
}

func encodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func getImageDimensions(imgPath string) (int, int) {
	file, err := os.Open(imgPath)
	if err != nil {
		return 1000, 1000
	}
	defer file.Close()

	cfg, _, err := image.DecodeConfig(file)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 1000, 1000
	}
	return cfg.Width, cfg.Height
}

func callVertexForJSON(ctx context.Context, modelName string, text string, formatInstructions string, imagePaths []string) ([]WordItem, error) {
	token, projectID, location, isAPIKey := getVertexCredentials(ctx)
	if token == "" {
		return nil, fmt.Errorf("no credentials available for Vertex/Gemini API")
	}

	realWidth, realHeight := 1000, 1000
	if len(imagePaths) > 0 {
		realWidth, realHeight = getImageDimensions(imagePaths[0])
	}

	var prompt string
	if formatInstructions != "" {
		prompt = fmt.Sprintf(`You are a Korean-English vocabulary extraction AI.

TASK: Extract English headwords from the OCR text below and output a JSON array.
Each JSON object = ONE unique English headword with its Korean meaning.
I am also providing the source images — use them to determine accurate bounding boxes for each word.

=== FORMAT-SPECIFIC EXTRACTION INSTRUCTIONS (from image analysis) ===
%s

=== STRICT HEADWORD EXCLUSION RULES ===
1. ONLY MAIN HEADWORDS: Extract ONLY single primary headword entries (usually having an item number like 51, 52, 53).
2. NEVER EXTRACT EXAMPLE PHRASES/COLLOCATIONS: Expressions like "germ killer", "stage setting", "medical clinic", "pedestrian speech", "voucher for a free meal", "award a scholarship", "elevated road" are example phrases, NOT main headwords. DO NOT create separate entries for them!
3. NEVER EXTRACT DERIVED WORDS/ANNOTATIONS AS ENTRIES: Secondary notes under a main entry (e.g. "differ (v)", "delivery (n)", "effectively (adv)", "= consider", "≠ ineffective") MUST NOT become separate entries.
4. NEVER EXTRACT SENTENCES OR KOREAN TRANSLATIONS: Skip all example sentences and their Korean translations.

=== STRICT EXACT TEXT & MULTIPLE MEANINGS PRESERVATION RULE (CRITICAL) ===
- EXTRACT ALL PRINTED KOREAN MEANINGS: If an entry has multiple Korean meanings separated by commas or spaces (e.g., "신기하게, 신비하게"), you MUST extract ALL meanings in full, separated by commas. DO NOT omit or drop any secondary definition! (e.g., output "신기하게, 신비하게", NEVER truncate to just "신비하게"!).
- MULTIMODAL IMAGE CROSS-CHECK & OCR CORRECTION: The text transcript may contain OCR reading errors or typos. ALWAYS look closely at the provided SOURCE IMAGES to verify the exact printed Korean definition! If the OCR text says one thing (e.g. "의심하다") but the SOURCE IMAGE clearly shows a different printed Korean word (e.g. "짐작하다"), ALWAYS prioritize and extract the EXACT printed Korean word visible in the SOURCE IMAGE!
- NO SYNONYM REPLACEMENT / NO PARAPHRASTING: DO NOT replace or substitute Korean meanings with similar or general synonyms (e.g., if the image says "조산사", you MUST output "조산사". DO NOT arbitrarily change it to "산부인과 의사" or any other synonym!).
- KEEP ORIGINAL TRANSLATIONS EXACTLY AS-IS: Preserve the original printed Korean definitions without altering, paraphrasing, or replacing them with your internal knowledge.

=== OUTPUT FORMAT & REAL SOURCE IMAGE RESOLUTION ===
The source image actual physical resolution is %dx%d pixels.
Output a JSON object containing:
- "imageWidth" (integer): Actual source image width (%d).
- "imageHeight" (integer): Actual source image height (%d).
- "words": Array of JSON objects, where each object contains:
  * "no" (integer): Original sequence number from OCR. Auto-increment from 1 if missing.
  * "word" (string): Clean English headword. Remove trailing punctuation.
  * "pos" (string): Korean POS abbreviation: "명"/"동"/"형"/"부"/"전"/"접". Infer from context. DO NOT default all to "명".
  * "meaning" (string): EXACT Korean meaning(s) ONLY from source image, comma-separated. DO NOT replace with synonyms. NO English text.
  * "bbox" (array of 4 integers): [ymin, xmin, ymax, xmax] relative to physical image resolution (%dx%d) or percentage 0-1000 scale. Look at actual source images.
  * "imageWidth" (integer): Actual image width (%d).
  * "imageHeight" (integer): Actual image height (%d).
  * "imageIndex" (integer): 1-based index indicating which image the word appears in.

=== IMAGE RESOLUTION & ASPECT RATIO SPECIFICATION ===
- Physical Image Dimensions: %dx%d pixels (Width x Height).
- Aspect Ratio: %.3f (Width / Height).
- CRITICAL ASPECT CORRECTION: This image is NOT square (%dx%d). Measure ymin and ymax strictly based on the un-stretched physical image canvas top. DO NOT push Y-coordinates further down as you go down the page!

=== DYNAMIC VISUAL ENTRY BLOCK BOUNDING BOX (BBOX) RULES ===
1. ABSOLUTE ZERO HARDCODING: Do NOT guess or use static preset numbers. You MUST dynamically inspect the actual image visual content for each image.
2. ENTIRE ENTRY BLOCK BOXING: The "bbox" array [ymin, xmin, ymax, xmax] (scaled 0-1000) MUST frame the FULL ENTRY BLOCK for each headword from its top English word line down to the bottom of its example sentence / definition block.
3. FULL ROW HEIGHT COVERAGE: Each vocabulary entry block naturally occupies approximately 120-150 units (12-15%%) of vertical height.
4. LAST ENTRY YMIN BOUNDARY: The 5th (last) entry on standard vocabulary pages starts around ymin 750-800 and ends by ymax 880-920. NEVER set ymin above 900 (90%%) for the last entry block, because ymin > 900 is reserved for page footers/numbers!

OCR Transcriptions:
%s`, formatInstructions, realWidth, realHeight, realWidth, realHeight, realWidth, realHeight, realWidth, realHeight, realWidth, realHeight, float64(realWidth)/float64(realHeight), realWidth, realHeight, text)
	} else {
		prompt = fmt.Sprintf(`Extract English vocabulary entries from text into JSON array with keys: "no", "word", "pos" (Korean 1-char abbreviation "명"/"동"/"형"/"부"/"전"/"접"), "meaning" (EXACT printed Korean meaning, comma-separated), "bbox" (array of 4 integers relative to %dx%d), "imageWidth" (%d), "imageHeight" (%d), and "imageIndex".

Text:
%s`, realWidth, realHeight, realWidth, realHeight, text)
	}

	// Build multimodal parts: images first, then text prompt
	var parts []map[string]interface{}
	for _, imgPath := range imagePaths {
		imgData, err := os.ReadFile(imgPath)
		if err != nil {
			fmt.Printf("[WARN] Stage 2: skip image %s: %v\n", imgPath, err)
			continue
		}
		mimeType := "image/jpeg"
		if strings.ToLower(filepath.Ext(imgPath)) == ".png" {
			mimeType = "image/png"
		}
		parts = append(parts, map[string]interface{}{
			"inline_data": map[string]interface{}{
				"mime_type": mimeType,
				"data":      encodeBase64(imgData),
			},
		})
	}
	parts = append(parts, map[string]interface{}{"text": prompt})

	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": parts,
			},
		},
		"generationConfig": map[string]interface{}{
			"response_mime_type": "application/json",
			"temperature":        0.1,
		},
	}

	jsonPayload, _ := json.Marshal(payload)
	if modelName == "" {
		modelName = os.Getenv("VERTEX_MODEL")
	}
	if modelName == "" {
		modelName = "gemini-2.5-flash"
	}

	endpoint := buildVertexEndpoint(modelName, location, projectID, token, isAPIKey)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthHeader(req, token, isAPIKey)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vertex api request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vertex api error (status %d): %s", resp.StatusCode, string(body))
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
		return nil, err
	}

	if len(resStruct.Candidates) == 0 || len(resStruct.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty text response from Vertex AI")
	}

	responseText := strings.TrimSpace(resStruct.Candidates[0].Content.Parts[0].Text)
	return parseStructuredJSONResponse(responseText)
}

func getBedrockBearerToken() string {
	token := os.Getenv("AWS_BEARER_TOKEN_BEDROCK")
	if token != "" {
		return token
	}
	if envData, err := os.ReadFile(".env"); err == nil {
		for _, line := range strings.Split(string(envData), "\n") {
			if strings.HasPrefix(line, "AWS_BEARER_TOKEN_BEDROCK=") {
				return strings.TrimSpace(strings.Split(line, "=")[1])
			}
		}
	}
	return ""
}

func callBedrockForJSON(ctx context.Context, modelID string, text string, formatInstructions string, imagePaths []string) ([]WordItem, error) {
	bearerToken := getBedrockBearerToken()
	if bearerToken == "" {
		return nil, fmt.Errorf("AWS_BEARER_TOKEN_BEDROCK not set in environment or .env")
	}

	if modelID == "" {
		modelID = "us.anthropic.claude-sonnet-4-5-20250929-v1:0"
	}

	realWidth, realHeight := 1000, 1000
	if len(imagePaths) > 0 {
		realWidth, realHeight = getImageDimensions(imagePaths[0])
	}

	var prompt string
	if formatInstructions != "" {
		prompt = fmt.Sprintf(`You are a Korean-English vocabulary extraction AI.

TASK: Extract English headwords from the OCR text below and output a JSON array.
Each JSON object = ONE unique English headword with its Korean meaning.

=== FORMAT-SPECIFIC EXTRACTION INSTRUCTIONS (from image analysis) ===
%s

=== STRICT HEADWORD EXCLUSION RULES ===
1. ONLY MAIN HEADWORDS: Extract ONLY single primary headword entries.
2. NEVER EXTRACT EXAMPLE PHRASES/COLLOCATIONS.
3. NEVER EXTRACT DERIVED WORDS/ANNOTATIONS AS ENTRIES.
4. NEVER EXTRACT SENTENCES OR KOREAN TRANSLATIONS.

=== STRICT EXACT TEXT & MULTIPLE MEANINGS PRESERVATION RULE (CRITICAL) ===
- EXTRACT ALL PRINTED KOREAN MEANINGS: If an entry has multiple Korean meanings separated by commas (e.g., "신기하게, 신비하게"), extract ALL meanings in full.
- MULTIMODAL IMAGE CROSS-CHECK & OCR CORRECTION: The text transcript may contain OCR reading errors or typos. ALWAYS look closely at the provided SOURCE IMAGES to verify the exact printed Korean definition! If the OCR text says one thing (e.g. "의심하다") but the SOURCE IMAGE clearly shows a different printed Korean word (e.g. "짐작하다"), ALWAYS prioritize and extract the EXACT printed Korean word visible in the SOURCE IMAGE!
- NO SYNONYM REPLACEMENT / NO PARAPHRASTING: DO NOT replace or substitute Korean meanings with synonyms.

=== OUTPUT FORMAT & BOUNDING BOX RULES ===
Output a JSON object containing:
- "imageWidth" (integer): Actual source image width (%d).
- "imageHeight" (integer): Actual source image height (%d).
- "words": Array of JSON objects, where each object contains:
  * "no" (integer): Original sequence number.
  * "word" (string): Clean English headword.
  * "pos" (string): Korean POS abbreviation: "명"/"동"/"형"/"부"/"전"/"접"/"관"/"감".
  * "meaning" (string): EXACT Korean meaning(s) ONLY from source image, comma-separated.
  * "bbox" (array of 4 integers): [ymin, xmin, ymax, xmax] as 0 to 1000 normalized integer coordinates covering the entire headword row (number, word, POS, and meaning).
  * "imageWidth" (integer): Actual image width (%d).
  * "imageHeight" (integer): Actual image height (%d).
  * "imageIndex" (integer): 1-based index indicating which image the word appears in.

=== IMAGE RESOLUTION & ASPECT RATIO SPECIFICATION ===
- Physical Image Dimensions: %dx%d pixels (Width x Height).
- Aspect Ratio: %.3f (Width / Height).
- CRITICAL ASPECT CORRECTION: This image is NOT square (%dx%d). Measure ymin and ymax strictly based on the un-stretched physical image canvas top. DO NOT push Y-coordinates further down as you go down the page!

=== DYNAMIC VISUAL ENTRY BLOCK BOUNDING BOX (BBOX) RULES ===
1. ABSOLUTE ZERO HARDCODING: Do NOT guess or use static preset numbers. You MUST dynamically inspect the actual image visual content for each image.
2. ENTIRE ENTRY BLOCK BOXING: The "bbox" array [ymin, xmin, ymax, xmax] (scaled 0-1000) MUST frame the FULL ENTRY BLOCK for each headword from its top English word line down to the bottom of its example sentence / definition block.
3. FULL ROW HEIGHT COVERAGE: Each vocabulary entry block naturally occupies approximately 120-150 units (12-15%%) of vertical height.
4. LAST ENTRY YMIN BOUNDARY: The 5th (last) entry on standard vocabulary pages starts around ymin 750-800 and ends by ymax 880-920. NEVER set ymin above 900 (90%%) for the last entry block, because ymin > 900 is reserved for page footers/numbers!

OCR Transcriptions:
%s

Return ONLY a raw JSON array or container object without markdown codeblock preambles.`, formatInstructions, realWidth, realHeight, realWidth, realHeight, realWidth, realHeight, float64(realWidth)/float64(realHeight), realWidth, realHeight, text)
	} else {
		prompt = fmt.Sprintf(`Extract English vocabulary entries from text into JSON array with keys: "no", "word", "pos" (Korean 1-char abbreviation "명"/"동"/"형"/"부"/"전"/"접"), "meaning" (EXACT printed Korean meaning, comma-separated), "bbox" (array of 4 integers 0-1000), "imageWidth" (%d), "imageHeight" (%d), and "imageIndex".

Text:
%s`, realWidth, realHeight, text)
	}

	var payload map[string]interface{}
	if strings.Contains(modelID, "claude") {
		var contentList []map[string]interface{}
		for _, imgPath := range imagePaths {
			imgBytes, err := os.ReadFile(imgPath)
			if err == nil {
				mimeType := "image/jpeg"
				if strings.HasSuffix(strings.ToLower(imgPath), ".png") {
					mimeType = "image/png"
				}
				contentList = append(contentList, map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"type":       "base64",
						"media_type": mimeType,
						"data":       base64.StdEncoding.EncodeToString(imgBytes),
					},
				})
			}
		}
		contentList = append(contentList, map[string]interface{}{
			"type": "text",
			"text": prompt,
		})

		payload = map[string]interface{}{
			"anthropic_version": "bedrock-2023-05-31",
			"max_tokens":        4096,
			"temperature":       0.1,
			"messages": []map[string]interface{}{
				{
					"role":    "user",
					"content": contentList,
				},
			},
		}
	} else {
		var contentList []map[string]interface{}
		for _, imgPath := range imagePaths {
			imgBytes, err := os.ReadFile(imgPath)
			if err == nil {
				format := "jpeg"
				if strings.HasSuffix(strings.ToLower(imgPath), ".png") {
					format = "png"
				}
				contentList = append(contentList, map[string]interface{}{
					"image": map[string]interface{}{
						"format": format,
						"source": map[string]interface{}{
							"bytes": base64.StdEncoding.EncodeToString(imgBytes),
						},
					},
				})
			}
		}
		contentList = append(contentList, map[string]interface{}{
			"text": prompt,
		})

		payload = map[string]interface{}{
			"messages": []map[string]interface{}{
				{
					"role":    "user",
					"content": contentList,
				},
			},
		}
	}

	endpoint := fmt.Sprintf("https://bedrock-runtime.us-east-1.amazonaws.com/model/%s/invoke", modelID)
	jsonBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bedrock HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var jsonText string
	if strings.Contains(modelID, "claude") {
		var claudeResp struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(respBody, &claudeResp); err == nil && len(claudeResp.Content) > 0 {
			jsonText = claudeResp.Content[0].Text
		}
	} else {
		var novaResp struct {
			Output struct {
				Message struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			} `json:"output"`
		}
		if err := json.Unmarshal(respBody, &novaResp); err == nil && len(novaResp.Output.Message.Content) > 0 {
			jsonText = novaResp.Output.Message.Content[0].Text
		}
	}

	return parseStructuredJSONResponse(jsonText)
}

func parseStructuredJSONResponse(responseText string) ([]WordItem, error) {
	responseText = strings.TrimSpace(responseText)
	if strings.HasPrefix(responseText, "```json") {
		responseText = strings.TrimPrefix(responseText, "```json")
		responseText = strings.TrimSuffix(responseText, "```")
	} else if strings.HasPrefix(responseText, "```") {
		responseText = strings.TrimPrefix(responseText, "```")
		responseText = strings.TrimSuffix(responseText, "```")
	}
	responseText = strings.TrimSpace(responseText)

	// 1. Try container object: { "imageWidth": 1000, "imageHeight": 1000, "words": [...] }
	var container struct {
		ImageWidth  int        `json:"imageWidth"`
		ImageHeight int        `json:"imageHeight"`
		Words       []WordItem `json:"words"`
	}
	if err := json.Unmarshal([]byte(responseText), &container); err == nil && len(container.Words) > 0 {
		wWidth := container.ImageWidth
		if wWidth <= 0 {
			wWidth = 1000
		}
		wHeight := container.ImageHeight
		if wHeight <= 0 {
			wHeight = 1000
		}
		for i := range container.Words {
			if container.Words[i].ImageWidth <= 0 {
				container.Words[i].ImageWidth = wWidth
			}
			if container.Words[i].ImageHeight <= 0 {
				container.Words[i].ImageHeight = wHeight
			}
		}
		return cleanWordItems(container.Words, 1000), nil
	}

	// 2. Direct array: [ {...}, {...} ]
	var words []WordItem
	if err := json.Unmarshal([]byte(responseText), &words); err == nil {
		for i := range words {
			if words[i].ImageWidth <= 0 {
				words[i].ImageWidth = 1000
			}
			if words[i].ImageHeight <= 0 {
				words[i].ImageHeight = 1000
			}
		}
		return cleanWordItems(words, 1000), nil
	}

	return nil, fmt.Errorf("failed to unmarshal JSON response: %s", responseText)
}

func callClaudeForJSON(ctx context.Context, apiKey, text string) ([]WordItem, error) {
	prompt := fmt.Sprintf(`Convert the following OCR English vocabulary text into a clean JSON array of objects with keys: "no" (number), "word" (string), "pos" (Korean single-character abbreviation like "형", "명", "동", "부", "전", "접", "관", "감"), and "meaning" (exact printed Korean meaning from text - DO NOT replace or paraphrase with synonyms like changing '조산사' to '산부인과 의사').

Text:
%s

Return ONLY valid JSON array without any markdown formatting or code block.`, text)

	payload := map[string]interface{}{
		"model":      "claude-3-5-sonnet-20241022",
		"max_tokens": 4096,
		"messages": []map[string]interface{}{
			{
				"role":    "user",
				"content": prompt,
			},
		},
	}

	jsonPayload, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error %d: %s", resp.StatusCode, string(body))
	}

	var resStruct struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &resStruct); err != nil {
		return nil, err
	}

	if len(resStruct.Content) == 0 {
		return nil, fmt.Errorf("empty content response")
	}

	responseText := strings.TrimSpace(resStruct.Content[0].Text)
	if strings.HasPrefix(responseText, "```") {
		lines := strings.Split(responseText, "\n")
		if len(lines) >= 2 {
			responseText = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	var words []WordItem
	if err := json.Unmarshal([]byte(responseText), &words); err != nil {
		return nil, err
	}
	return words, nil
}

func parseOCRTextFallback(text string, preserveOrder bool) []WordItem {
	lines := strings.Split(text, "\n")
	var words []WordItem
	reWithPos := regexp.MustCompile(`^(?:(\d+)[\.\s]+)?\s*([a-zA-Z\s\-]+?)\s*(?:\((.*?)\)|\[(.*?)\])\s*(?:=|:|-)?\s*(.*)$`)
	reSimple := regexp.MustCompile(`^(?:(\d+)[\.\s]+)?\s*([a-zA-Z\s\-]{2,})\s*(?:=|:|-)?\s*(.*)$`)

	no := 1
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Filter out preambles, introductory phrases, and example sentences
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "here is") || strings.HasPrefix(lower, "here are") || strings.HasPrefix(lower, "here's") || strings.HasPrefix(lower, "the following") || strings.HasPrefix(lower, "example") || strings.HasPrefix(lower, "* example") || strings.HasPrefix(lower, "*example") {
			continue
		}

		// Clean leading markdown bullet/number formatting and asterisks
		cleanLine := strings.ReplaceAll(line, "**", "")
		cleanLine = strings.TrimLeft(cleanLine, "* -#=≠>")
		cleanLine = strings.TrimSpace(cleanLine)

		cleanLower := strings.ToLower(cleanLine)
		if strings.HasPrefix(cleanLower, "example") || strings.HasPrefix(cleanLower, "example sentence") || strings.HasPrefix(cleanLower, "example phrase") || strings.HasPrefix(cleanLower, "korean:") {
			continue
		}

		var lineNoStr, wordStr, rawPos, meaningStr string

		if matches := reWithPos.FindStringSubmatch(cleanLine); len(matches) > 0 {
			lineNoStr = matches[1]
			wordStr = matches[2]
			rawPos = matches[3]
			if rawPos == "" {
				rawPos = matches[4]
			}
			meaningStr = matches[5]
		} else if matches := reSimple.FindStringSubmatch(cleanLine); len(matches) > 0 {
			lineNoStr = matches[1]
			wordStr = matches[2]
			meaningStr = matches[3]
		}

		wordStr = strings.TrimSpace(wordStr)
		wordStr = strings.Trim(wordStr, "*_#:. ")

		lineNo, _ := strconv.Atoi(lineNoStr)
		if !preserveOrder || lineNo <= 0 {
			lineNo = no
		}

		// Skip if wordStr is empty or looks like a preamble word
		wordLower := strings.ToLower(wordStr)
		if wordStr == "" || len(wordStr) <= 1 || wordLower == "here" || wordLower == "heres" || wordLower == "example" || wordLower == "transcription" || wordLower == "korean" {
			continue
		}

		pos := NormalizePOS(rawPos)
		rawPosLower := strings.ToLower(rawPos)
		if strings.Contains(rawPosLower, "v") {
			pos = "동"
		} else if strings.Contains(rawPosLower, "adj") || strings.Contains(rawPosLower, "a") {
			pos = "형"
		} else if strings.Contains(rawPosLower, "adv") || strings.Contains(rawPosLower, "ad") {
			pos = "부"
		} else if strings.Contains(rawPosLower, "n") {
			pos = "명"
		}

		words = append(words, WordItem{
			No:      lineNo,
			Word:    wordStr,
			Pos:     pos,
			Meaning: strings.TrimSpace(meaningStr),
		})
		no++
	}
	return words
}

func cleanWordItems(words []WordItem, targetBBoxScale int) []WordItem {
	if targetBBoxScale <= 0 {
		targetBBoxScale = 1000
	}

	var cleaned []WordItem
	fallbackNo := 1
	seenWords := make(map[string]bool)

	for _, w := range words {
		// 1. Clean Word
		wordStr := strings.TrimSpace(w.Word)
		wordStr = strings.ReplaceAll(wordStr, "**", "")
		wordStr = strings.ReplaceAll(wordStr, "*", "")
		wordStr = strings.Trim(wordStr, " :;\"'()-_#.=≠>")

		reNumPrefix := regexp.MustCompile(`^\d+[\.\s]+`)
		wordStr = reNumPrefix.ReplaceAllString(wordStr, "")

		lowerWord := strings.ToLower(wordStr)
		if wordStr == "" || len(wordStr) <= 1 {
			continue
		}
		// Strict Sentence & Noise Filter: exclude sentences (>= 3 spaces or length > 35) & artifacts
		if strings.Count(wordStr, " ") >= 3 || len(wordStr) > 35 {
			continue
		}
		if lowerWord == "unit" || lowerWord == "vocabulary list" || lowerWord == "day" || lowerWord == "page" || lowerWord == "here" || lowerWord == "here's" || lowerWord == "heres" || lowerWord == "example" || lowerWord == "transcription" || lowerWord == "korean" {
			continue
		}
		if strings.HasPrefix(lowerWord, "here is") || strings.HasPrefix(lowerWord, "here are") || strings.HasPrefix(lowerWord, "example sentence") || strings.HasPrefix(lowerWord, "example phrase") {
			continue
		}

		if seenWords[lowerWord] {
			continue
		}
		seenWords[lowerWord] = true

		itemNo := w.No
		if itemNo <= 0 {
			itemNo = fallbackNo
		}
		fallbackNo = itemNo + 1

		posStr := NormalizePOS(w.Pos)

		meaningStr := ""
		switch m := w.Meaning.(type) {
		case string:
			meaningStr = strings.TrimSpace(m)
		case []interface{}:
			var items []string
			for _, item := range m {
				items = append(items, fmt.Sprintf("%v", item))
			}
			meaningStr = strings.Join(items, ", ")
		}

		meaningStr = strings.ReplaceAll(meaningStr, "**", "")
		meaningStr = strings.TrimLeft(meaningStr, " :;-=≠>")

		cleaned = append(cleaned, WordItem{
			No:         itemNo,
			Word:       wordStr,
			Pos:        posStr,
			Meaning:    meaningStr,
			BBox:       w.BBox,
			ImageIndex: w.ImageIndex,
			ImageName:  w.ImageName,
		})
	}

	sort.SliceStable(cleaned, func(i, j int) bool {
		return cleaned[i].No < cleaned[j].No
	})

	// Strictly normalize all BBox coordinates to 0-100 Percentage Scale
	maxVal := 0
	for _, w := range cleaned {
		for _, v := range w.BBox {
			if v > maxVal {
				maxVal = v
			}
		}
	}

	if maxVal > 100 {
		for i := range cleaned {
			for j := range cleaned[i].BBox {
				cleaned[i].BBox[j] /= 10
			}
		}
	}

	totalCount := len(cleaned)
	for i := range cleaned {
		bbox := cleaned[i].BBox
		isInvalid := len(bbox) < 4 || (bbox[0] == 0 && bbox[1] == 0 && bbox[2] == 0 && bbox[3] == 0) || bbox[2] <= bbox[0] || bbox[3] <= bbox[1]

		if isInvalid {
			topPct := 8
			if totalCount > 1 {
				topPct = 8 + int((float64(i)/float64(totalCount-1))*78.0)
			}
			heightPct := 5
			bottomPct := topPct + heightPct
			if bottomPct > 98 {
				bottomPct = 98
			}

			if targetBBoxScale == 1000 {
				cleaned[i].BBox = []int{topPct * 10, 50, bottomPct * 10, 950}
			} else {
				cleaned[i].BBox = []int{topPct, 5, bottomPct, 95}
			}
		}
	}

	// Assign differential sequential created timestamps for exact order preservation
	baseTime := time.Now().Add(-time.Duration(totalCount) * time.Second)
	for i := range cleaned {
		itemTime := baseTime.Add(time.Duration(i) * time.Second)
		cleaned[i].Created = itemTime.Format("2006-01-02 15:04:05")
	}

	return cleaned
}

func GenerateDocFile(words []WordItem, outputPath string) error {
	var html bytes.Buffer
	html.WriteString(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>Vocat Vocabulary Test Sheet</title>
<style>
  body { font-family: 'Malgun Gothic', sans-serif; margin: 20px; }
  h1 { text-align: center; color: #2b3674; }
  table { width: 100%; border-collapse: collapse; margin-top: 20px; }
  th, td { border: 1px solid #cbd5e1; padding: 10px; text-align: left; }
  th { background-color: #f1f5f9; color: #1e293b; }
  tr:nth-child(even) { background-color: #f8fafc; }
  .pos { font-weight: bold; color: #4318ff; text-align: center; width: 60px; }
  .no { text-align: center; width: 50px; }
</style>
</head>
<body>
<h1>Vocat 단어 시험지 및 정답표</h1>
<table>
  <thead>
    <tr>
      <th class="no">No</th>
      <th>영어 단어 (Word)</th>
      <th class="pos">품사</th>
      <th>한국어 의미 (Meaning)</th>
    </tr>
  </thead>
  <tbody>
`)

	baseTime := time.Now().Add(-time.Duration(len(words)) * time.Second)
	for idx, w := range words {
		meaningText := ""
		switch m := w.Meaning.(type) {
		case string:
			meaningText = m
		case []interface{}:
			strList := lo.Map(m, func(item interface{}, _ int) string {
				return fmt.Sprintf("%v", item)
			})
			meaningText = strings.Join(strList, ", ")
		}

		createdTime := w.Created
		if createdTime == "" {
			createdTime = baseTime.Add(time.Duration(idx) * time.Second).Format("2006-01-02 15:04:05")
		}

		fmt.Fprintf(&html, `    <tr data-no="%d" data-created="%s">
      <td class="no">%d</td>
      <td><strong>%s</strong></td>
      <td class="pos">%s</td>
      <td>%s</td>
    </tr>
`, w.No, createdTime, w.No, w.Word, w.Pos, meaningText)
	}

	html.WriteString(`  </tbody>
</table>
</body>
</html>`)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, html.Bytes(), 0o644)
}
