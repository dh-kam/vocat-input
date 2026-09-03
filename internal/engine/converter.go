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
	_ "golang.org/x/image/webp"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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

var genericTitles = map[string]bool{
	"null": true, "none": true, "untitled": true, "n/a": true, "unknown": true,
	"vocabulary": true, "words": true, "단어장": true, "단어목록": true, "단어시험": true,
}

const trimCutset = " \"'“”‘’`:;-"

// CleanTitle trims, removes markdown wrapping, prefixes, and filters generic names.
func CleanTitle(raw string) string {
	t := strings.TrimSpace(raw)
	t = strings.ReplaceAll(t, "**", "")
	t = strings.ReplaceAll(t, "*", "")
	t = strings.ReplaceAll(t, "`", "")
	t = strings.Trim(t, trimCutset)

	prefixes := []string{
		"Title:", "title:", "TITLE:",
		"교재명:", "제목:", "단원명:", "교재:",
		"Textbook:", "Material:",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(t, p) {
			t = strings.TrimSpace(strings.TrimPrefix(t, p))
			t = strings.Trim(t, trimCutset)
		}
	}

	// Strip prompt leakage if prompt instructions leaked into title
	promptLeakPhrases := []string{
		"INDEX NUMBERS", "INDEX NUMBER", "YOU MUST", "You MUST", "you must",
		"CRITICAL RULES", "PRESERVE ALL", "EXACT TEXT ONLY",
		"STRICT HEADWORD", "FORMAT-SPECIFIC", "OUTPUT FORMAT",
	}
	for _, phrase := range promptLeakPhrases {
		if idx := strings.Index(strings.ToUpper(t), strings.ToUpper(phrase)); idx != -1 {
			t = strings.TrimSpace(t[:idx])
			t = strings.Trim(t, trimCutset)
		}
	}

	fields := strings.Fields(t)
	t = strings.Join(fields, " ")

	if genericTitles[strings.ToLower(t)] || len(t) < 2 {
		return ""
	}
	if len([]rune(t)) > 80 {
		runes := []rune(t)
		t = string(runes[:80])
	}
	return t
}

// SanitizeFileName cleans file path separators and illegal characters for OS file naming.
func SanitizeFileName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '\n', '\r', '\t':
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	res := strings.TrimSpace(b.String())
	res = strings.Trim(res, ". ")
	if res == "" {
		return "Vocat_Material"
	}
	return res
}

func cleanStructuringResult(res *StructuringResult, imagePaths ...[]string) *StructuringResult {
	if res == nil {
		return &StructuringResult{Words: []WordItem{}}
	}
	res.Title = CleanTitle(res.Title)
	if len(imagePaths) > 0 && len(imagePaths[0]) > 0 {
		res.Words = attachImageMetadata(res.Words, imagePaths[0])
	}
	res.Words = cleanWordItems(res.Words)
	return res
}

func ConvertOCRToVocatJSON(ctx context.Context, mergedText string, preserveOrder bool, imagePaths []string, ocrProvider string, ocrModel string) (*StructuringResult, error) {
	fmt.Printf("[AI Structuring Engine] Launching with Provider: '%s', Model: '%s'\n", ocrProvider, ocrModel)

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

	// Stage 2: Extract structured JSON using selected Provider & Model.
	prov := strings.ToLower(strings.TrimSpace(ocrProvider))
	modelLower := strings.ToLower(strings.TrimSpace(ocrModel))
	isClaudeModel := strings.Contains(modelLower, "claude") ||
		strings.Contains(modelLower, "opus") ||
		strings.Contains(modelLower, "sonnet") ||
		strings.Contains(modelLower, "fable") ||
		strings.Contains(modelLower, "haiku")

	// If a Claude/Opus/Sonnet model was requested (under any provider), route to Bedrock/Anthropic
	if isClaudeModel {
		bedrockModelID := resolveClaudeBedrockModelID(ocrModel)
		fmt.Printf("[AI Structuring Stage 2] Claude/Opus/Sonnet Model '%s' requested (resolved to '%s'). Calling Bedrock/Anthropic...\n", ocrModel, bedrockModelID)
		res, err := callBedrockForJSON(ctx, bedrockModelID, mergedText, formatInstructions, imagePaths)
		if err == nil && res != nil {
			if cleaned := cleanStructuringResult(res, imagePaths); len(cleaned.Words) > 0 {
				fmt.Printf("[AI Structuring Stage 2 Success] Extracted %d structured words with Claude '%s' (Title: '%s')\n", len(cleaned.Words), ocrModel, cleaned.Title)
				return cleaned, nil
			}
		}
		if apiKey := LookupConfig("ANTHROPIC_API_KEY"); apiKey != "" {
			res, err := callClaudeForJSON(ctx, apiKey, mergedText)
			if err == nil && res != nil {
				if cleaned := cleanStructuringResult(res, imagePaths); len(cleaned.Words) > 0 {
					return cleaned, nil
				}
			}
		}
		fmt.Printf("[AI Structuring Warning] Claude/Bedrock Model '%s' call failed (%v). Trying Google AI Studio/Vertex fallback...\n", ocrModel, err)
	}

	if prov == "google-ai-studio" || prov == "google_ai_studio" || prov == "ai-studio" || prov == "gemini" {
		fmt.Printf("[AI Structuring Stage 2] Calling Google AI Studio Gemini Model '%s'...\n", ocrModel)
		res, err := callGoogleAIStudioForJSON(ctx, ocrModel, mergedText, formatInstructions, imagePaths)
		if err == nil && res != nil {
			if cleaned := cleanStructuringResult(res, imagePaths); len(cleaned.Words) > 0 {
				fmt.Printf("[AI Structuring Stage 2 Success] Extracted %d structured words with Google AI Studio '%s' (Title: '%s')\n", len(cleaned.Words), ocrModel, cleaned.Title)
				return cleaned, nil
			}
		}
		fmt.Printf("[AI Structuring Warning] Google AI Studio Model '%s' call failed or empty (%v). Trying Vertex fallback...\n", ocrModel, err)
	}

	if prov == "bedrock" {
		fmt.Printf("[AI Structuring Stage 2] Calling AWS Bedrock Model '%s'...\n", ocrModel)
		res, err := callBedrockForJSON(ctx, ocrModel, mergedText, formatInstructions, imagePaths)
		if err == nil && res != nil {
			if cleaned := cleanStructuringResult(res, imagePaths); len(cleaned.Words) > 0 {
				fmt.Printf("[AI Structuring Stage 2 Success] Extracted %d structured words with Bedrock '%s' (Title: '%s')\n", len(cleaned.Words), ocrModel, cleaned.Title)
				return cleaned, nil
			}
		}
		fmt.Printf("[AI Structuring Warning] Bedrock Model '%s' call failed or empty (%v). Trying Vertex fallback...\n", ocrModel, err)
	}

	// Default / Vertex AI Engine
	fmt.Printf("[AI Structuring Stage 2] Calling GCP Vertex AI Model '%s'...\n", ocrModel)
	res, err := callVertexForJSON(ctx, ocrModel, mergedText, formatInstructions, imagePaths)
	if err == nil && res != nil {
		if cleaned := cleanStructuringResult(res, imagePaths); len(cleaned.Words) > 0 {
			fmt.Printf("[AI Structuring Stage 2 Success] Extracted %d structured words with Vertex '%s' (Title: '%s')\n", len(cleaned.Words), ocrModel, cleaned.Title)
			return cleaned, nil
		}
	}

	// 3. Fallback: Anthropic Direct API
	apiKey := LookupConfig("ANTHROPIC_API_KEY")
	if apiKey != "" {
		res, err := callClaudeForJSON(ctx, apiKey, mergedText)
		if err == nil && res != nil {
			if cleaned := cleanStructuringResult(res, imagePaths); len(cleaned.Words) > 0 {
				return cleaned, nil
			}
		}
	}

	// 4. Regex Fallback Parser
	fallbackWords, fallbackTitle := parseOCRTextFallbackWithTitle(mergedText, preserveOrder)
	res = cleanStructuringResult(&StructuringResult{
		Title: fallbackTitle,
		Words: fallbackWords,
	}, imagePaths)
	if len(res.Words) == 0 {
		return res, fmt.Errorf("no vocabulary words could be extracted from OCR text")
	}
	return res, nil
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
6. What is the overall textbook title, unit/day name, chapter, or test title printed in the header/top area of the page? (e.g., '절대수능맵 VOCA Inter 3 Vocabulary Test(기본)', 'DAY 01', 'UNIT 10', '워드마스터 수능 2000 Day 12' etc.)

Then write EXTRACTION INSTRUCTIONS — a clear, specific prompt section that tells an AI:
- Exactly how to identify a single main headword entry in this specific format
- STRICT EXCLUSION: Explicitly list what to SKIP (example phrases/collocations e.g. 'germ killer', example sentences, Korean translations of sentences, derived forms)
- How to extract the main word, POS, and Korean meaning for each entry
- STRICT PRESERVATION: Require keeping the EXACT printed Korean definitions from the image without replacing them with similar/synonym words (e.g., preserve "조산사", NEVER replace with "산부인과 의사")
- How to preserve original numbering
- How to extract the overall title into the 'title' field combining textbook name, day/unit, and test type if present

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
	modelName := LookupConfig("VERTEX_MODEL")
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

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return "", RedactedError("format analysis request failed: %s", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
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
	projectID = LookupConfig("GCP_PROJECT_ID")
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

	location = LookupConfig("GCP_LOCATION")
	if location == "" {
		location = "us-central1"
	}

	// 1. OAuth token flow (Vertex AI with full GCP project access)
	token = LookupConfig("GCLOUD_ACCESS_TOKEN")
	if token == "" {
		cmd := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token")
		out, err := cmd.Output()
		if err == nil {
			token = strings.TrimSpace(string(out))
			if idx := strings.Index(token, "\n"); idx != -1 {
				token = strings.TrimSpace(token[:idx])
			}
		}
	}

	if token != "" {
		isAPIKey = false
		return
	}

	// 2. Vertex AI API key fallback
	token = LookupConfig("VERTEX_AI_API_KEY", "VERTEX_API_KEY")
	if token != "" {
		isAPIKey = true
		return
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

type imageMeta struct {
	width  int
	height int
	name   string
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

func collectImageMeta(imagePaths []string) map[int]imageMeta {
	m := make(map[int]imageMeta)
	for i, p := range imagePaths {
		idx := i + 1 // 1-indexed
		w, h := getImageDimensions(p)
		m[idx] = imageMeta{
			width:  w,
			height: h,
			name:   filepath.Base(p),
		}
	}
	return m
}

func attachImageMetadata(words []WordItem, imagePaths []string) []WordItem {
	if len(imagePaths) == 0 {
		return words
	}
	metaMap := collectImageMeta(imagePaths)
	defaultMeta := metaMap[1]

	for i := range words {
		idx := words[i].ImageIndex
		meta, ok := metaMap[idx]
		if !ok {
			meta = defaultMeta
			if words[i].ImageIndex <= 0 {
				words[i].ImageIndex = 1
			}
		}
		words[i].ImageName = meta.name

		// AI Canvas vs Physical Image Dimensions Fallback:
		// If AI model provided its canvas dimensions (ImageWidth/ImageHeight > 0), preserve them
		// to allow accurate ratio back-calculation of bounding boxes.
		// If AI model did NOT provide canvas dimensions (<= 0), fallback to physical image dimensions.
		if words[i].ImageWidth <= 0 {
			words[i].ImageWidth = meta.width
		}
		if words[i].ImageHeight <= 0 {
			words[i].ImageHeight = meta.height
		}
	}
	return words
}

func callVertexForJSON(ctx context.Context, modelName string, text string, formatInstructions string, imagePaths []string) (*StructuringResult, error) {
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
I am also providing the source images — use them to determine accurate bounding boxes for each word and identify the textbook title from headers.

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

=== MATERIAL TITLE EXTRACTION RULE ===
- Inspect the top header area of the page/images for the textbook name, day/unit number, chapter name, or test title (e.g. "절대수능맵 VOCA Inter 3 Vocabulary Test(기본) DAY 01", "워드마스터 수능 2000 Day 15", "EBS 수능특강 영어 Day 03").
- Combine the book title, unit/day, and test type into a clean, concise title string in the root "title" property.
- If no clear header title is found, set "title" to an empty string ("").

=== OUTPUT FORMAT & CANVAS DIMENSION SPECIFICATION ===
The source image actual physical resolution is %dx%d pixels (Width x Height).
Output a JSON object containing:
- "title" (string): Overall textbook name and unit/day (e.g. "절대수능맵 VOCA Inter 3 Vocabulary Test(기본) DAY 01").
- "imageWidth" (integer): Canvas width (%d).
- "imageHeight" (integer): Canvas height (%d).
- "words": Array of JSON objects, where each object contains:
  * "no" (integer): Original sequence number from OCR. Auto-increment from 1 if missing.
  * "word" (string): Clean English headword. Remove trailing punctuation.
  * "pos" (string): Korean POS abbreviation: "명"/"동"/"형"/"부"/"전"/"접". Infer from context. DO NOT default all to "명".
  * "meaning" (string): EXACT Korean meaning(s) ONLY from source image, comma-separated. DO NOT replace with synonyms. NO English text.
  * "bbox" (array of 4 integers): [ymin, xmin, ymax, xmax] measured in literal pixels of [0, 0, %d, %d] (or exact 0-1000 scale). Frame the full horizontal entry row containing the number, word, POS, and printed Korean meaning.
  * "imageWidth" (integer): Canvas width (%d).
  * "imageHeight" (integer): Canvas height (%d).
  * "imageIndex" (integer): 1-based index indicating which image the word appears in.

=== CRITICAL COORDINATE ANCHORING & NO INTERNAL CROPPING (STRICT) ===
1. ABSOLUTELY NO INTERNAL CROPPING, PADDING, OR RESCALING:
   - You are provided with an image of exact dimensions %d pixels (width) x %d pixels (height).
   - DO NOT crop margins, borders, or change the coordinate origin!
   - [0, 0] is strictly the literal top-left corner (pixel 0, 0) of this provided %dx%d image file.
   - [%d, %d] is strictly the literal bottom-right corner (pixel %d, %d) of this image file.
2. TABLE STRUCTURE & VISUAL GROUNDING:
   - Top Header Area: The page title, textbook name, date, and student name appear at the top.
   - Table Column Header Row: The table column headers row ('번호', '단어', '품사', '뜻') is a HEADER, NOT a vocabulary entry! DO NOT assign a bbox or entry to it!
   - First Word Row: Word #1 is strictly on the row BELOW the table column header! DO NOT shift row coordinates upward!
3. ENTIRE ENTRY ROW/BLOCK BOXING:
   - Each word's "bbox": [ymin, xmin, ymax, xmax] MUST tightly frame the entire horizontal row of that vocabulary item (from the item number on the left, across the English headword and POS, to the end of the printed Korean meaning on the right).
   - On 2-column sheets, frame the full column row width (left column: xmin ~ 4%% to xmax ~ 49%%; right column: xmin ~ 51%% to xmax ~ 96%%).
   - Aspect Ratio: %.3f (Width / Height). Measure ymin and ymax strictly based on your canvas coordinates without aspect distortion.

OCR Transcriptions:
%s`, formatInstructions, realWidth, realHeight, realWidth, realHeight, realHeight, realWidth, realWidth, realHeight, realWidth, realHeight, realWidth, realHeight, realHeight, realWidth, realHeight, realWidth, float64(realWidth)/float64(realHeight), text)
	} else {
		prompt = fmt.Sprintf(`Extract English vocabulary entries from text into JSON object with "title" (textbook/unit title from header if any), "imageWidth" (%d), "imageHeight" (%d), and "words" array with keys: "no", "word", "pos" (Korean 1-char abbreviation "명"/"동"/"형"/"부"/"전"/"접"), "meaning" (EXACT printed Korean meaning, comma-separated), "bbox" (array of 4 integers [ymin, xmin, ymax, xmax] in pixels of [0, 0, %d, %d], tightly framing the full horizontal entry row, NOT the column header), "imageWidth" (%d), "imageHeight" (%d), and "imageIndex".

Text:
%s`, realWidth, realHeight, realHeight, realWidth, realWidth, realHeight, text)
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
	modelName = MapVertexModelName(modelName)

	endpoint := buildVertexEndpoint(modelName, location, projectID, token, isAPIKey)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthHeader(req, token, isAPIKey)

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, RedactedError("vertex api request failed: %s", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[Vertex AI Structuring Warning] API error (status %d): %s. Attempting Bedrock fallback...\n", resp.StatusCode, string(body))
		if bRes, bErr := callBedrockForJSON(ctx, "amazon.nova-pro-v1:0", text, formatInstructions, imagePaths); bErr == nil && bRes != nil {
			return bRes, nil
		}
		if bRes, bErr := callBedrockForJSON(ctx, "us.anthropic.claude-sonnet-4-6", text, formatInstructions, imagePaths); bErr == nil && bRes != nil {
			return bRes, nil
		}
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

func callGoogleAIStudioForJSON(ctx context.Context, modelName string, text string, formatInstructions string, imagePaths []string) (*StructuringResult, error) {
	apiKey := getGoogleAIStudioAuth()
	if apiKey == "" {
		return nil, fmt.Errorf("no API key available for Google AI Studio (set GOOGLE_AI_STUDIO_API_KEY or GEMINI_API_KEY in .env)")
	}

	realWidth, realHeight := 1000, 1000
	if len(imagePaths) > 0 {
		rw, rh := getImageDimensions(imagePaths[0])
		if rw > 0 && rh > 0 {
			realWidth, realHeight = rw, rh
		}
	}

	var prompt string
	if formatInstructions != "" {
		prompt = fmt.Sprintf(`You are a Korean-English vocabulary extraction AI.

TASK: Extract English headwords from the OCR text below and output a JSON array.
Each JSON object = ONE unique English headword with its Korean meaning.
I am also providing the source images — use them to determine accurate bounding boxes for each word and identify the textbook title from headers.

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

=== MATERIAL TITLE EXTRACTION RULE ===
- Inspect the top header area of the page/images for the textbook name, day/unit number, chapter name, or test title (e.g. "절대수능맵 VOCA Inter 3 Vocabulary Test(기본) DAY 01", "워드마스터 수능 2000 Day 15", "EBS 수능특강 영어 Day 03").
- Combine the book title, unit/day, and test type into a clean, concise title string in the root "title" property.
- If no clear header title is found, set "title" to an empty string ("").

=== OUTPUT FORMAT & CANVAS DIMENSION SPECIFICATION ===
The source image actual physical resolution is %dx%d pixels (Width x Height).
Output a JSON object containing:
- "title" (string): Overall textbook name and unit/day (e.g. "절대수능맵 VOCA Inter 3 Vocabulary Test(기본) DAY 01").
- "imageWidth" (integer): Canvas width (%d).
- "imageHeight" (integer): Canvas height (%d).
- "words": Array of JSON objects, where each object contains:
  * "no" (integer): Original sequence number from OCR. Auto-increment from 1 if missing.
  * "word" (string): Clean English headword. Remove trailing punctuation.
  * "pos" (string): Korean POS abbreviation: "명"/"동"/"형"/"부"/"전"/"접". Infer from context. DO NOT default all to "명".
  * "meaning" (string): EXACT Korean meaning(s) ONLY from source image, comma-separated. DO NOT replace with synonyms. NO English text.
  * "bbox" (array of 4 integers): [ymin, xmin, ymax, xmax] measured in literal pixels of [0, 0, %d, %d] (or exact 0-1000 scale). Frame the full horizontal entry row containing the number, word, POS, and printed Korean meaning.
  * "imageWidth" (integer): Canvas width (%d).
  * "imageHeight" (integer): Canvas height (%d).
  * "imageIndex" (integer): 1-based index indicating which image the word appears in.

=== CRITICAL COORDINATE ANCHORING & NO INTERNAL CROPPING (STRICT) ===
1. ABSOLUTELY NO INTERNAL CROPPING, PADDING, OR RESCALING:
   - You are provided with an image of exact dimensions %d pixels (width) x %d pixels (height).
   - DO NOT crop margins, borders, or change the coordinate origin!
   - [0, 0] is strictly the literal top-left corner (pixel 0, 0) of this provided %dx%d image file.
   - [%d, %d] is strictly the literal bottom-right corner (pixel %d, %d) of this image file.
2. TABLE STRUCTURE & VISUAL GROUNDING:
   - Top Header Area: The page title, textbook name, date, and student name appear at the top.
   - Table Column Header Row: The table column headers row ('번호', '단어', '품사', '뜻') is a HEADER, NOT a vocabulary entry! DO NOT assign a bbox or entry to it!
   - First Word Row: Word #1 is strictly on the row BELOW the table column header! DO NOT shift row coordinates upward!
3. ENTIRE ENTRY ROW/BLOCK BOXING:
   - Each word's "bbox": [ymin, xmin, ymax, xmax] MUST tightly frame the entire horizontal row of that vocabulary item (from the item number on the left, across the English headword and POS, to the end of the printed Korean meaning on the right).
   - On 2-column sheets, frame the full column row width (left column: xmin ~ 4%% to xmax ~ 49%%; right column: xmin ~ 51%% to xmax ~ 96%%).
   - Aspect Ratio: %.3f (Width / Height). Measure ymin and ymax strictly based on your canvas coordinates without aspect distortion.

OCR Transcriptions:
%s`, formatInstructions, realWidth, realHeight, realWidth, realHeight, realHeight, realWidth, realWidth, realHeight, realWidth, realHeight, realWidth, realHeight, realHeight, realWidth, realHeight, realWidth, float64(realWidth)/float64(realHeight), text)
	} else {
		prompt = fmt.Sprintf(`Extract English vocabulary entries from text into JSON object with "title" (textbook/unit title from header if any), "imageWidth" (%d), "imageHeight" (%d), and "words" array with keys: "no", "word", "pos" (Korean 1-char abbreviation "명"/"동"/"형"/"부"/"전"/"접"), "meaning" (EXACT printed Korean meaning, comma-separated), "bbox" (array of 4 integers [ymin, xmin, ymax, xmax] in pixels of [0, 0, %d, %d], tightly framing the full horizontal entry row, NOT the column header), "imageWidth" (%d), "imageHeight" (%d), and "imageIndex".

Text:
%s`, realWidth, realHeight, realHeight, realWidth, realWidth, realHeight, text)
	}

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
		modelName = LookupConfig("GEMINI_MODEL", "GOOGLE_AI_STUDIO_MODEL")
	}
	if modelName == "" || strings.Contains(strings.ToLower(modelName), "claude") {
		modelName = "gemini-2.5-flash"
	}

	endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", modelName, apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, RedactedError("Google AI Studio API request failed: %s", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("[Google AI Studio Structuring Warning] API error (status %d): %s. Attempting Vertex AI / Bedrock fallback...\n", resp.StatusCode, string(body))
		if vRes, vErr := callVertexForJSON(ctx, "gemini-2.5-flash", text, formatInstructions, imagePaths); vErr == nil && vRes != nil {
			return vRes, nil
		}
		if bRes, bErr := callBedrockForJSON(ctx, "amazon.nova-pro-v1:0", text, formatInstructions, imagePaths); bErr == nil && bRes != nil {
			return bRes, nil
		}
		return nil, fmt.Errorf("Google AI Studio API error (status %d): %s", resp.StatusCode, string(body))
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
		return nil, fmt.Errorf("unmarshal Google AI Studio response: %w", err)
	}

	if len(resStruct.Candidates) == 0 || len(resStruct.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("empty text response from Google AI Studio")
	}

	responseText := strings.TrimSpace(resStruct.Candidates[0].Content.Parts[0].Text)
	return parseStructuredJSONResponse(responseText)
}

func getBedrockBearerToken() string {
	return LookupConfig("AWS_BEARER_TOKEN_BEDROCK")
}

func resolveClaudeBedrockModelID(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(m, "fable"):
		return "us.anthropic.claude-4-5-fable-v1:0"
	case strings.Contains(m, "4-6-opus") || strings.Contains(m, "4.6-opus") || strings.Contains(m, "opus-4-6") || strings.Contains(m, "opus-4.6"):
		return "us.anthropic.claude-opus-4-6"
	case strings.Contains(m, "4-5-opus") || strings.Contains(m, "4.5-opus") || strings.Contains(m, "opus-4-5") || strings.Contains(m, "opus-4.5"):
		return "us.anthropic.claude-opus-4-5"
	case strings.Contains(m, "3-opus") || strings.Contains(m, "3.0-opus") || strings.Contains(m, "opus-3"):
		return "us.anthropic.claude-3-opus-20240229-v1:0"
	case strings.Contains(m, "opus"):
		return "us.anthropic.claude-opus-4-6"
	case strings.Contains(m, "4-6") || strings.Contains(m, "4.6") || strings.Contains(m, "sonnet-4-6") || strings.Contains(m, "sonnet-4.6"):
		return "us.anthropic.claude-sonnet-4-6"
	case strings.Contains(m, "4-5") || strings.Contains(m, "4.5") || strings.Contains(m, "sonnet-4-5") || strings.Contains(m, "sonnet-4.5"):
		return "us.anthropic.claude-sonnet-4-5-20250929-v1:0"
	case strings.Contains(m, "3-7") || strings.Contains(m, "3.7") || strings.Contains(m, "sonnet-3-7") || strings.Contains(m, "sonnet-3.7"):
		return "us.anthropic.claude-3-7-sonnet-20250219-v1:0"
	case strings.Contains(m, "3-5-sonnet") || strings.Contains(m, "3.5-sonnet"):
		return "us.anthropic.claude-3-5-sonnet-20241022-v2:0"
	case strings.Contains(m, "sonnet"):
		return "us.anthropic.claude-sonnet-4-6"
	case strings.Contains(m, "haiku"):
		return "us.anthropic.claude-3-5-haiku-20241022-v1:0"
	case strings.HasPrefix(m, "us.anthropic.") || strings.HasPrefix(m, "anthropic.") || strings.HasPrefix(m, "amazon."):
		return model
	default:
		return "us.anthropic.claude-sonnet-4-6"
	}
}

func callBedrockForJSON(ctx context.Context, modelID string, text string, formatInstructions string, imagePaths []string) (*StructuringResult, error) {
	bearerToken := getBedrockBearerToken()
	if bearerToken == "" {
		return nil, fmt.Errorf("AWS_BEARER_TOKEN_BEDROCK not set in environment or .env")
	}

	modelID = resolveClaudeBedrockModelID(modelID)

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

=== MATERIAL TITLE EXTRACTION RULE ===
- Inspect the top header area of the page/images for the textbook name, day/unit number, chapter name, or test title (e.g. "절대수능맵 VOCA Inter 3 Vocabulary Test(기본) DAY 01", "워드마스터 수능 2000 Day 15", "EBS 수능특강 영어 Day 03").
- Combine the book title, unit/day, and test type into a clean, concise title string in the root "title" property.
- If no clear header title is found, set "title" to an empty string ("").

=== OUTPUT FORMAT & CANVAS DIMENSION SPECIFICATION ===
The source image actual physical resolution is %dx%d pixels (Width x Height).
Output a JSON object containing:
- "title" (string): Overall textbook name and unit/day (e.g. "절대수능맵 VOCA Inter 3 Vocabulary Test(기본) DAY 01").
- "imageWidth" (integer): Canvas width (%d).
- "imageHeight" (integer): Canvas height (%d).
- "words": Array of JSON objects, where each object contains:
  * "no" (integer): Original sequence number.
  * "word" (string): Clean English headword.
  * "pos" (string): Korean POS abbreviation: "명"/"동"/"형"/"부"/"전"/"접"/"관"/"감".
  * "meaning" (string): EXACT Korean meaning(s) ONLY from source image, comma-separated.
  * "bbox" (array of 4 integers): [ymin, xmin, ymax, xmax] measured in literal pixels of [0, 0, %d, %d] (or exact 0-1000 scale). Frame the full horizontal entry row containing the number, word, POS, and printed Korean meaning.
  * "imageWidth" (integer): Canvas width (%d).
  * "imageHeight" (integer): Canvas height (%d).
  * "imageIndex" (integer): 1-based index indicating which image the word appears in.

=== CRITICAL COORDINATE ANCHORING & NO INTERNAL CROPPING (STRICT) ===
1. ABSOLUTELY NO INTERNAL CROPPING, PADDING, OR RESCALING:
   - You are provided with an image of exact dimensions %d pixels (width) x %d pixels (height).
   - DO NOT crop margins, borders, or change the coordinate origin!
   - [0, 0] is strictly the literal top-left corner (pixel 0, 0) of this provided %dx%d image file.
   - [%d, %d] is strictly the literal bottom-right corner (pixel %d, %d) of this image file.
2. TABLE STRUCTURE & VISUAL GROUNDING:
   - Top Header Area: The page title, textbook name, date, and student name appear at the top.
   - Table Column Header Row: The table column headers row ('번호', '단어', '품사', '뜻') is a HEADER, NOT a vocabulary entry! DO NOT assign a bbox or entry to it!
   - First Word Row: Word #1 is strictly on the row BELOW the table column header! DO NOT shift row coordinates upward!
3. ENTIRE ENTRY ROW/BLOCK BOXING:
   - Each word's "bbox": [ymin, xmin, ymax, xmax] MUST tightly frame the entire horizontal row of that vocabulary item (from the item number on the left, across the English headword and POS, to the end of the printed Korean meaning on the right).
   - On 2-column sheets, frame the full column row width (left column: xmin ~ 4%% to xmax ~ 49%%; right column: xmin ~ 51%% to xmax ~ 96%%).
   - Aspect Ratio: %.3f (Width / Height). Measure ymin and ymax strictly based on your canvas coordinates without aspect distortion.

OCR Transcriptions:
%s

Return ONLY a raw JSON array or container object without markdown codeblock preambles.`, formatInstructions, realWidth, realHeight, realWidth, realHeight, realHeight, realWidth, realWidth, realHeight, realWidth, realHeight, realWidth, realHeight, realHeight, realWidth, realHeight, realWidth, float64(realWidth)/float64(realHeight), text)
	} else {
		prompt = fmt.Sprintf(`Extract English vocabulary entries from text into JSON object with "title" (header title if any), "imageWidth" (%d), "imageHeight" (%d), and "words" array with keys: "no", "word", "pos" (Korean 1-char abbreviation "명"/"동"/"형"/"부"/"전"/"접"), "meaning" (EXACT printed Korean meaning, comma-separated), "bbox" (array of 4 integers [ymin, xmin, ymax, xmax] in pixels of [0, 0, %d, %d], tightly framing the full horizontal entry row, NOT the column header), "imageWidth" (%d), "imageHeight" (%d), and "imageIndex".

Text:
%s`, realWidth, realHeight, realHeight, realWidth, realWidth, realHeight, text)
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

	region := LookupConfig("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	endpoint := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/invoke", region, modelID)
	jsonBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
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

func extractJSONSubstring(text string) string {
	text = strings.TrimSpace(text)
	// Strip markdown blocks if present
	if strings.Contains(text, "```json") {
		start := strings.Index(text, "```json") + 7
		end := strings.LastIndex(text, "```")
		if end > start {
			text = strings.TrimSpace(text[start:end])
		}
	} else if strings.Contains(text, "```") {
		start := strings.Index(text, "```") + 3
		end := strings.LastIndex(text, "```")
		if end > start {
			text = strings.TrimSpace(text[start:end])
		}
	}

	// Find outermost balanced JSON object or array that parses successfully
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch == '{' || ch == '[' {
			openCh := ch
			closeCh := byte('}')
			if openCh == '[' {
				closeCh = ']'
			}
			depth := 0
			inString := false
			escape := false

			for j := i; j < len(text); j++ {
				c := text[j]
				if escape {
					escape = false
					continue
				}
				if c == '\\' {
					escape = true
					continue
				}
				if c == '"' {
					inString = !inString
					continue
				}
				if inString {
					continue
				}

				if c == openCh {
					depth++
				} else if c == closeCh {
					depth--
					if depth == 0 {
						candidate := text[i : j+1]
						var js json.RawMessage
						if json.Unmarshal([]byte(candidate), &js) == nil {
							return candidate
						}
					}
				}
			}
		}
	}
	return text
}

func parseStructuredJSONResponse(responseText string) (*StructuringResult, error) {
	responseText = extractJSONSubstring(responseText)

	// 1. Try container object: { "title": "...", "imageWidth": ..., "imageHeight": ..., "words": [...] }
	var container struct {
		Title        string     `json:"title"`
		ImageWidth   int        `json:"imageWidth"`
		ImageHeight  int        `json:"imageHeight"`
		CanvasWidth  int        `json:"canvasWidth"`
		CanvasHeight int        `json:"canvasHeight"`
		Words        []WordItem `json:"words"`
	}
	if err := json.Unmarshal([]byte(responseText), &container); err == nil && len(container.Words) > 0 {
		wWidth := container.ImageWidth
		if wWidth <= 0 && container.CanvasWidth > 0 {
			wWidth = container.CanvasWidth
		}
		wHeight := container.ImageHeight
		if wHeight <= 0 && container.CanvasHeight > 0 {
			wHeight = container.CanvasHeight
		}

		for i := range container.Words {
			if container.Words[i].ImageWidth <= 0 && wWidth > 0 {
				container.Words[i].ImageWidth = wWidth
			}
			if container.Words[i].ImageHeight <= 0 && wHeight > 0 {
				container.Words[i].ImageHeight = wHeight
			}
		}
		return &StructuringResult{
			Title:       CleanTitle(container.Title),
			ImageWidth:  wWidth,
			ImageHeight: wHeight,
			Words:       container.Words,
		}, nil
	}

	// 2. Direct array: [ {...}, {...} ]
	var words []WordItem
	if err := json.Unmarshal([]byte(responseText), &words); err == nil && len(words) > 0 {
		return &StructuringResult{
			Title: "",
			Words: words,
		}, nil
	}

	return nil, fmt.Errorf("failed to unmarshal JSON response: %s", responseText)
}

func callClaudeForJSON(ctx context.Context, apiKey, text string) (*StructuringResult, error) {
	prompt := fmt.Sprintf(`Convert the following OCR English vocabulary text into a clean JSON object with "title" (textbook or unit title in header if present) and "words" array of objects with keys: "no" (number), "word" (string), "pos" (Korean single-character abbreviation like "형", "명", "동", "부", "전", "접", "관", "감"), and "meaning" (exact printed Korean meaning from text - DO NOT replace or paraphrase with synonyms like changing '조산사' to '산부인과 의사').

Text:
%s

Return ONLY valid JSON object with "title" and "words" without any markdown formatting or code block.`, text)

	payload := map[string]interface{}{
		"model":      anthropicModel(),
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

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
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

	return parseStructuredJSONResponse(resStruct.Content[0].Text)
}

func parseOCRTextFallbackWithTitle(text string, preserveOrder bool) ([]WordItem, string) {
	words := parseOCRTextFallback(text, preserveOrder)
	lines := strings.Split(text, "\n")
	var extractedTitle string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		lower := strings.ToLower(l)
		if strings.Contains(lower, "day ") || strings.Contains(lower, "unit ") || strings.Contains(l, "VOCA") || strings.Contains(l, "수능") {
			extractedTitle = CleanTitle(l)
			if extractedTitle != "" {
				break
			}
		}
	}
	return words, extractedTitle
}

func parseOCRTextFallback(text string, preserveOrder bool) []WordItem {
	lines := strings.Split(text, "\n")
	var words []WordItem
	reWithPos := regexp.MustCompile(`^(?:(\d+)[\.\s]+)?\s*([a-zA-Z\s\-]+?)\s*(?:\((.*?)\)|\[(.*?)\])\s*(?:=|:|-)?\s*(.*)$`)
	reSimple := regexp.MustCompile(`^(?:(\d+)[\.\s]+)?\s*([a-zA-Z\s\-]{2,})\s*(?:=|:|-)?\s*(.*)$`)

	no := 1
	reOnlyNum := regexp.MustCompile(`^\d+$`)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Support Markdown table rows (e.g., "| 1 | 1 | tide | 조수 | 36 | significant | 중요한 |")
		if strings.Contains(line, "|") {
			cells := strings.Split(line, "|")
			var cleanCells []string
			for _, c := range cells {
				tc := strings.TrimSpace(c)
				if tc != "" && tc != "---" && !strings.HasPrefix(tc, "--") {
					cleanCells = append(cleanCells, tc)
				}
			}
			if len(cleanCells) == 0 {
				continue
			}

			// Skip table header rows if all numeric or dashes
			allNumHeader := true
			for _, c := range cleanCells {
				if !reOnlyNum.MatchString(c) {
					allNumHeader = false
					break
				}
			}
			if allNumHeader && len(cleanCells) > 4 {
				continue
			}

			for cIdx := 0; cIdx < len(cleanCells); cIdx++ {
				cell := cleanCells[cIdx]
				// Pattern 1: [num, num_duplicate, english, korean] (e.g. "1", "1", "tide", "조수")
				if cIdx+3 < len(cleanCells) && reOnlyNum.MatchString(cell) && reOnlyNum.MatchString(cleanCells[cIdx+1]) && isAlphaWord(cleanCells[cIdx+2]) && hasHangul(cleanCells[cIdx+3]) {
					num, _ := strconv.Atoi(cell)
					w := cleanCells[cIdx+2]
					m := cleanCells[cIdx+3]
					words = append(words, WordItem{
						No:      num,
						Word:    w,
						Pos:     "명",
						Meaning: m,
					})
					cIdx += 3
					continue
				}
				// Pattern 2: [num, english, korean] (e.g. "36", "significant", "중요한")
				if cIdx+2 < len(cleanCells) && reOnlyNum.MatchString(cell) && isAlphaWord(cleanCells[cIdx+1]) && hasHangul(cleanCells[cIdx+2]) {
					num, _ := strconv.Atoi(cell)
					w := cleanCells[cIdx+1]
					m := cleanCells[cIdx+2]
					words = append(words, WordItem{
						No:      num,
						Word:    w,
						Pos:     "명",
						Meaning: m,
					})
					cIdx += 2
					continue
				}
				// Pattern 3: [english, korean] (e.g. "tide", "조수")
				if cIdx+1 < len(cleanCells) && isAlphaWord(cell) && hasHangul(cleanCells[cIdx+1]) {
					words = append(words, WordItem{
						No:      no,
						Word:    cell,
						Pos:     "명",
						Meaning: cleanCells[cIdx+1],
					})
					no++
					cIdx++
					continue
				}
			}
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
		if pos == "" {
			rawPosLower := strings.ToLower(strings.Trim(rawPos, "()[]{}., "))
			switch {
			case strings.HasPrefix(rawPosLower, "adv") || rawPosLower == "ad":
				pos = "부"
			case strings.HasPrefix(rawPosLower, "adj") || rawPosLower == "a":
				pos = "형"
			case strings.HasPrefix(rawPosLower, "v") || rawPosLower == "vi" || rawPosLower == "vt":
				pos = "동"
			case strings.HasPrefix(rawPosLower, "n"):
				pos = "명"
			case strings.HasPrefix(rawPosLower, "prep"):
				pos = "전"
			case strings.HasPrefix(rawPosLower, "conj"):
				pos = "접"
			default:
				pos = "명"
			}
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

func isAlphaWord(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return false
	}
	hasLetter := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '-' || r == ' ' || r == '\'' || r == '(' || r == ')' {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				hasLetter = true
			}
			continue
		}
		return false
	}
	return hasLetter
}

func hasHangul(s string) bool {
	for _, r := range s {
		if (r >= 0xAC00 && r <= 0xD7A3) || (r >= 0x1100 && r <= 0x11FF) || (r >= 0x3130 && r <= 0x318F) {
			return true
		}
	}
	return false
}

// BBoxOutputScale is the single coordinate space every WordItem.BBox leaves the engine in:
// integer percentages of the source image, ordered [ymin, xmin, ymax, xmax].
const BBoxOutputScale = 100

// detectBBoxScale reports the maximum a coordinate can reach in the incoming boxes, which is
// what a coordinate has to be measured against to become a percentage.
//
// The extraction prompts ask for 0-1000 normalized coordinates but also permit raw pixels
// relative to imageWidth/imageHeight, and the per-provider scale is not knowable from the model
// name (the Bedrock prompt, for instance, asks for 0-1000). So the incoming scale is detected
// rather than assumed. Detection looks at the whole batch because a single box near the
// top-left corner is indistinguishable from a percentage box on its own.
//
// A refMax of BBoxOutputScale means the boxes are already percentages and normalization is a
// no-op — that is what makes a second pass over already-clean items safe.
func detectBBoxScale(words []WordItem) (refMax float64, pixels bool) {
	maxVal := 0
	for i := range words {
		for _, v := range words[i].BBox {
			if v > maxVal {
				maxVal = v
			}
		}
	}
	switch {
	case maxVal <= BBoxOutputScale:
		return BBoxOutputScale, false
	case maxVal <= 1000:
		return 1000, false
	default:
		return float64(maxVal), true
	}
}

// normalizeBBoxes rewrites every box to BBoxOutputScale in place, grouped per image.
//
// Boxes that arrive valid stay valid: rounding a very thin box could otherwise flatten it into
// a degenerate one, which the caller would then mistake for missing data and overwrite with a
// placeholder.
func normalizeBBoxes(words []WordItem) {
	// Group words by ImageIndex so multi-image runs scale against their respective image frame
	groups := make(map[int][]int)
	for i := range words {
		idx := words[i].ImageIndex
		if idx <= 0 {
			idx = 1
		}
		groups[idx] = append(groups[idx], i)
	}

	toPct := func(v int, ref float64) int {
		p := int(math.Round(float64(v) * BBoxOutputScale / ref))
		if p < 0 {
			return 0
		}
		if p > BBoxOutputScale {
			return BBoxOutputScale
		}
		return p
	}

	for _, indices := range groups {
		var groupWords []WordItem
		for _, idx := range indices {
			groupWords = append(groupWords, words[idx])
		}

		refMax, pixels := detectBBoxScale(groupWords)
		if refMax == BBoxOutputScale {
			continue
		}

		for _, idx := range indices {
			bbox := words[idx].BBox
			if len(bbox) < 4 {
				continue
			}

			// Determine X and Y reference canvas dimensions:
			// Priority 1: AI-specified canvas dimensions (or fallback physical dimensions)
			// Priority 2: Fallback to detected batch reference scale
			refY, refX := refMax, refMax
			if words[idx].ImageHeight > 0 && words[idx].ImageWidth > 0 {
				refY = float64(words[idx].ImageHeight)
				refX = float64(words[idx].ImageWidth)
			} else if pixels {
				if h := words[idx].ImageHeight; h > 0 {
					refY = float64(h)
				}
				if w := words[idx].ImageWidth; w > 0 {
					refX = float64(w)
				}
			}

			wasValid := bbox[2] > bbox[0] && bbox[3] > bbox[1]
			bbox[0], bbox[2] = toPct(bbox[0], refY), toPct(bbox[2], refY)
			bbox[1], bbox[3] = toPct(bbox[1], refX), toPct(bbox[3], refX)

			if wasValid {
				if bbox[2] <= bbox[0] {
					bbox[0], bbox[2] = thinSpan(bbox[0])
				}
				if bbox[3] <= bbox[1] {
					bbox[1], bbox[3] = thinSpan(bbox[1])
				}

				// If the box only covers the word column (e.g. width < 25%) on a multi-column sheet,
				// expand the horizontal bounds to frame the full entry row including number and Korean meaning
				boxWidth := bbox[3] - bbox[1]
				if boxWidth < 25 {
					if bbox[1] < 50 {
						// Left column
						if bbox[1] > 5 {
							bbox[1] = 4
						}
						if bbox[3] < 46 {
							bbox[3] = 49
						}
					} else {
						// Right column
						if bbox[1] > 54 {
							bbox[1] = 51
						}
						if bbox[3] < 92 {
							bbox[3] = 96
						}
					}
				}
			}
		}
	}
}

// thinSpan widens a span that rounding collapsed to zero into the smallest visible one,
// staying inside 0..BBoxOutputScale.
func thinSpan(lo int) (int, int) {
	if lo >= BBoxOutputScale {
		return BBoxOutputScale - 1, BBoxOutputScale
	}
	return lo, lo + 1
}

// copyBBox isolates the caller's array and drops anything past the four coordinates the format
// defines. A stray fifth element would otherwise keep its original magnitude through
// normalization and then re-trigger scale detection on the next pass.
func copyBBox(b FlexibleBBox) FlexibleBBox {
	if len(b) > 4 {
		b = b[:4]
	}
	return append(FlexibleBBox(nil), b...)
}

// bboxIsUsable reports whether a box is a well-formed rectangle already on BBoxOutputScale.
// Anything else — too short, out of range, zero-area, inverted — is replaced by a placeholder,
// which is what keeps the output invariant unconditional: coordinates always land in
// 0..BBoxOutputScale and always describe a non-empty rectangle.
func bboxIsUsable(bbox FlexibleBBox) bool {
	if len(bbox) < 4 {
		return false
	}
	for _, v := range bbox {
		if v < 0 || v > BBoxOutputScale {
			return false
		}
	}
	if bbox[0] == 0 && bbox[1] == 0 && bbox[2] == 0 && bbox[3] == 0 {
		return false
	}
	return bbox[2] > bbox[0] && bbox[3] > bbox[1]
}

// cleanWordItems filters noise, normalizes fields, and returns items whose bounding boxes
// are always on BBoxOutputScale.
//
// It must stay idempotent: cleanWordItems(cleanWordItems(x)) has to equal cleanWordItems(x).
// An earlier version broke that in two ways — it wrote placeholder boxes on the 0-1000 scale
// while normalizing real ones to 0-100, so a second pass saw a max coordinate above 100 and
// divided every box by 10 again; and it rescaled the caller's BBox arrays in place, so the
// damage stuck to the input too. Both are why run_1785714340533 ended up with 18 boxes
// collapsed into the 0-10 range and 12 replaced by placeholders.
func cleanWordItems(words []WordItem) []WordItem {
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
		meaningStr = strings.TrimSpace(meaningStr)

		// Discard items with empty meaning or meaningless string
		if meaningStr == "" || meaningStr == "<nil>" {
			continue
		}

		// Discard form header words without real Korean definitions (e.g. "Date", "Name", "Score", "Class", "Vocabulary Test")
		if lowerWord == "date" || lowerWord == "name" || lowerWord == "score" || lowerWord == "class" || lowerWord == "student" || lowerWord == "test" || lowerWord == "page" || lowerWord == "total" {
			if strings.EqualFold(meaningStr, wordStr) || strings.EqualFold(meaningStr, "null") || strings.EqualFold(meaningStr, "none") {
				continue
			}
		}

		cleaned = append(cleaned, WordItem{
			No:      itemNo,
			Word:    wordStr,
			Pos:     posStr,
			Meaning: meaningStr,
			// Copy the box: normalizeBBoxes rewrites it, and the caller's slice must not move.
			BBox: copyBBox(w.BBox),
			// ImageWidth/ImageHeight are the reference frame a pixel-scale box is relative
			// to, so they have to survive the rebuild for normalization to interpret it.
			ImageWidth:  w.ImageWidth,
			ImageHeight: w.ImageHeight,
			ImageIndex:  w.ImageIndex,
			ImageName:   w.ImageName,
		})
	}

	sort.SliceStable(cleaned, func(i, j int) bool {
		return cleaned[i].No < cleaned[j].No
	})

	normalizeBBoxes(cleaned)

	totalCount := len(cleaned)
	for i := range cleaned {
		if bboxIsUsable(cleaned[i].BBox) {
			continue
		}

		// Evenly spaced placeholder so a row with no usable box is still selectable in the
		// evidence viewer. It goes out on BBoxOutputScale like every other box — writing it
		// on a different scale is what made a second pass corrupt the real boxes.
		topPct := 8
		if totalCount > 1 {
			topPct = 8 + int((float64(i)/float64(totalCount-1))*78.0)
		}
		bottomPct := topPct + 5
		if bottomPct > 98 {
			bottomPct = 98
		}
		cleaned[i].BBox = []int{topPct, 5, bottomPct, 95}
	}

	// Assign differential sequential created timestamps for exact order preservation
	baseTime := time.Now().Add(-time.Duration(totalCount) * time.Second)
	for i := range cleaned {
		itemTime := baseTime.Add(time.Duration(i) * time.Second)
		cleaned[i].Created = itemTime.Format("2006-01-02 15:04:05")
	}

	return cleaned
}

// GenerateDocFile has been moved to vocat_export.go
