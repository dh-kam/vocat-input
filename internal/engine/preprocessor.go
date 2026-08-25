package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
)

// PreprocessResult contains metadata about the preprocessed image.
type PreprocessResult struct {
	OriginalWidth  int  `json:"originalWidth"`
	OriginalHeight int  `json:"originalHeight"`
	NewWidth       int  `json:"newWidth"`
	NewHeight      int  `json:"newHeight"`
	RotationAngle  int  `json:"rotationAngle"` // 0, 90, 180, 270
	Enhanced       bool `json:"enhanced"`
}

// DetectImageOrientation uses multimodal AI vision to determine if the image needs rotation
// (0, 90, 180, 270 degrees clockwise) so that text reads horizontally from top to bottom.
func DetectImageOrientation(ctx context.Context, imgPath string) (int, error) {
	token, projectID, location, isAPIKey := getVertexCredentials(ctx)
	if token == "" {
		// If Vertex is not configured, check Bedrock
		return detectImageOrientationBedrock(ctx, imgPath)
	}

	// Create a fast, lightweight thumbnail for orientation detection (max 768px)
	srcImg, err := imaging.Open(imgPath, imaging.AutoOrientation(true))
	if err != nil {
		return 0, fmt.Errorf("failed to open image for orientation check: %w", err)
	}
	thumb := imaging.Fit(srcImg, 768, 768, imaging.Linear)
	var thumbBuf bytes.Buffer
	if err := imaging.Encode(&thumbBuf, thumb, imaging.JPEG, imaging.JPEGQuality(80)); err != nil {
		return 0, fmt.Errorf("failed to encode orientation thumbnail: %w", err)
	}

	base64Data := base64.StdEncoding.EncodeToString(thumbBuf.Bytes())

	promptText := `Analyze the text reading orientation in this image.
To make the text upright (reading horizontally from top to bottom, left to right like a standard printed book), by what angle in degrees clockwise MUST the image be rotated?
Choose exactly one integer from: 0, 90, 180, 270.
- 0: Already upright (normal reading orientation)
- 90: Rotated 90 degrees clockwise (needs 90° clockwise rotation or was taken sideways)
- 180: Upside down (needs 180° rotation)
- 270: Rotated 270 degrees clockwise (needs 270° clockwise rotation)

Return ONLY valid JSON matching this schema:
{"rotation": 0}`

	payload := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{
						"inlineData": map[string]interface{}{
							"mimeType": "image/jpeg",
							"data":     base64Data,
						},
					},
					{
						"text": promptText,
					},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":     0.0,
			"maxOutputTokens": 64,
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	modelName := LookupConfig("VERTEX_MODEL")
	if modelName == "" {
		modelName = "gemini-2.5-flash"
	}

	endpoint := buildVertexEndpoint(modelName, location, projectID, token, isAPIKey)

	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	setAuthHeader(req, token, isAPIKey)

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return 0, RedactedError("orientation detection request failed: %s", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("orientation detection error (status %d): %s", resp.StatusCode, string(body))
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
		return 0, err
	}
	if len(resStruct.Candidates) == 0 || len(resStruct.Candidates[0].Content.Parts) == 0 {
		return 0, fmt.Errorf("empty orientation response")
	}

	rawText := strings.TrimSpace(resStruct.Candidates[0].Content.Parts[0].Text)
	return parseRotationResponse(rawText)
}

func detectImageOrientationBedrock(ctx context.Context, imgPath string) (int, error) {
	bearerToken := LookupConfig("AWS_BEARER_TOKEN_BEDROCK")
	if bearerToken == "" {
		// No AI credentials available, assume 0
		return 0, nil
	}

	srcImg, err := imaging.Open(imgPath, imaging.AutoOrientation(true))
	if err != nil {
		return 0, err
	}
	thumb := imaging.Fit(srcImg, 768, 768, imaging.Linear)
	var thumbBuf bytes.Buffer
	if err := imaging.Encode(&thumbBuf, thumb, imaging.JPEG, imaging.JPEGQuality(80)); err != nil {
		return 0, err
	}

	base64Data := base64.StdEncoding.EncodeToString(thumbBuf.Bytes())

	region := LookupConfig("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	modelID := LookupConfig("BEDROCK_MODEL")
	if modelID == "" {
		modelID = "us.anthropic.claude-3-5-sonnet-20241022-v2:0"
	}

	promptText := `Analyze the text reading orientation in this image. To make the text upright (reading horizontally from top to bottom, left to right), by what angle in degrees clockwise MUST the image be rotated? Choose exactly one: 0, 90, 180, 270. Return ONLY JSON: {"rotation": 0}`

	endpoint := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/invoke", region, modelID)

	var payload map[string]interface{}
	if strings.Contains(modelID, "claude") {
		payload = map[string]interface{}{
			"anthropic_version": "bedrock-2023-05-31",
			"max_tokens":        64,
			"temperature":       0.0,
			"messages": []map[string]interface{}{
				{
					"role": "user",
					"content": []map[string]interface{}{
						{
							"type": "image",
							"source": map[string]interface{}{
								"type":       "base64",
								"media_type": "image/jpeg",
								"data":       base64Data,
							},
						},
						{
							"type": "text",
							"text": promptText,
						},
					},
				},
			},
		}
	} else {
		payload = map[string]interface{}{
			"messages": []map[string]interface{}{
				{
					"role": "user",
					"content": []map[string]interface{}{
						{
							"image": map[string]interface{}{
								"format": "jpeg",
								"source": map[string]interface{}{
									"bytes": base64Data,
								},
							},
						},
						{
							"text": promptText,
						},
					},
				},
			},
			"inferenceConfig": map[string]interface{}{
				"max_new_tokens": 64,
				"temperature":    0.0,
			},
		}
	}

	jsonPayload, _ := json.Marshal(payload)
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", endpoint, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bearerToken))

	resp, err := defaultHTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bedrock orientation error (status %d): %s", resp.StatusCode, string(body))
	}

	var resMap map[string]interface{}
	if err := json.Unmarshal(body, &resMap); err != nil {
		return 0, err
	}

	// Extract text
	var rawText string
	if content, ok := resMap["content"].([]interface{}); ok && len(content) > 0 {
		if first, ok := content[0].(map[string]interface{}); ok {
			if text, ok := first["text"].(string); ok {
				rawText = text
			}
		}
	}
	if rawText == "" {
		if output, ok := resMap["output"].(map[string]interface{}); ok {
			if msg, ok := output["message"].(map[string]interface{}); ok {
				if content, ok := msg["content"].([]interface{}); ok && len(content) > 0 {
					if first, ok := content[0].(map[string]interface{}); ok {
						if text, ok := first["text"].(string); ok {
							rawText = text
						}
					}
				}
			}
		}
	}

	return parseRotationResponse(rawText)
}

func parseRotationResponse(rawText string) (int, error) {
	// Extract JSON substring if wrapped in markdown
	jsonStr := extractJSONSubstring(rawText)
	if jsonStr == "" {
		jsonStr = rawText
	}

	var parsed struct {
		Rotation int `json:"rotation"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		// Fallback: search for numbers 90, 180, 270, 0
		if strings.Contains(rawText, "90") {
			return 90, nil
		}
		if strings.Contains(rawText, "180") {
			return 180, nil
		}
		if strings.Contains(rawText, "270") {
			return 270, nil
		}
		return 0, nil
	}

	switch parsed.Rotation {
	case 90, 180, 270:
		return parsed.Rotation, nil
	default:
		return 0, nil
	}
}

// PreprocessImage reads an image from imgPath, applies EXIF auto-orientation, detects and applies
// AI-based reading rotation (0°, 90°, 180°, 270°), normalizes dimensions to standard high-resolution bounds
// (max 2048px preserving aspect ratio), enhances document contrast & sharpness, and saves the result back.
func PreprocessImage(ctx context.Context, imgPath string, autoRotate bool) (PreprocessResult, error) {
	// 1. Open with EXIF auto-orientation
	img, err := imaging.Open(imgPath, imaging.AutoOrientation(true))
	if err != nil {
		return PreprocessResult{}, fmt.Errorf("failed to open image %s: %w", imgPath, err)
	}

	origBounds := img.Bounds()
	origW, origH := origBounds.Dx(), origBounds.Dy()

	rotationAngle := 0
	if autoRotate {
		angle, err := DetectImageOrientation(ctx, imgPath)
		if err == nil && angle != 0 {
			rotationAngle = angle
			switch angle {
			case 90:
				img = imaging.Rotate90(img)
			case 180:
				img = imaging.Rotate180(img)
			case 270:
				img = imaging.Rotate270(img)
			}
		}
	}

	// 2. Normalization & Resizing: Target max dimension 2048px (standard high-res book page)
	curBounds := img.Bounds()
	curW, curH := curBounds.Dx(), curBounds.Dy()
	const maxDim = 2048

	if curW > maxDim || curH > maxDim {
		img = imaging.Fit(img, maxDim, maxDim, imaging.Lanczos)
	}

	// 3. Document Quality Enhancement (Scan-like crispness)
	// Slight contrast boost (+8%) to whiten background paper and darken text
	img = imaging.AdjustContrast(img, 8)
	// Gentle unsharp mask sharpening (sigma 0.5) to crisp character boundaries
	img = imaging.Sharpen(img, 0.5)

	newBounds := img.Bounds()
	newW, newH := newBounds.Dx(), newBounds.Dy()

	// 4. Save enhanced image back to imgPath (high quality JPEG)
	// If original was PNG/WebP, convert to JPEG for consistent high-performance OCR ingestion
	ext := strings.ToLower(filepath.Ext(imgPath))
	if ext == ".png" || ext == ".webp" {
		if err := imaging.Save(img, imgPath); err != nil {
			return PreprocessResult{}, fmt.Errorf("failed to save preprocessed image %s: %w", imgPath, err)
		}
	} else {
		if err := imaging.Save(img, imgPath, imaging.JPEGQuality(95)); err != nil {
			return PreprocessResult{}, fmt.Errorf("failed to save preprocessed image %s: %w", imgPath, err)
		}
	}

	return PreprocessResult{
		OriginalWidth:  origW,
		OriginalHeight: origH,
		NewWidth:       newW,
		NewHeight:      newH,
		RotationAngle:  rotationAngle,
		Enhanced:       true,
	}, nil
}

// PreprocessImages processes all images in imagePaths in sequence, calling onProgress for each image.
func PreprocessImages(ctx context.Context, imagePaths []string, autoRotate bool, onProgress func(index, total int, res PreprocessResult, err error)) ([]PreprocessResult, error) {
	var results []PreprocessResult
	total := len(imagePaths)
	for i, path := range imagePaths {
		if ctx.Err() != nil {
			return results, ctx.Err()
		}
		res, err := PreprocessImage(ctx, path, autoRotate)
		results = append(results, res)
		if onProgress != nil {
			onProgress(i+1, total, res, err)
		}
	}
	return results, nil
}
