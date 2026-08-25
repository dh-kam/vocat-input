package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"vocat-input/internal/engine"
)

func callDirectVisionOCR(ctx context.Context, cropPath string) (string, error) {
	bearerToken := engine.LookupConfig("AWS_BEARER_TOKEN_BEDROCK")
	if bearerToken == "" {
		return "", fmt.Errorf("AWS_BEARER_TOKEN_BEDROCK not set")
	}

	imgBytes, err := os.ReadFile(cropPath)
	if err != nil {
		return "", err
	}

	mimeType := "image/png"
	if strings.HasSuffix(strings.ToLower(cropPath), ".jpg") || strings.HasSuffix(strings.ToLower(cropPath), ".jpeg") {
		mimeType = "image/jpeg"
	}

	prompt := "Read and list all English text visible in this cropped image snippet. Output only the plain English text."

	payload := map[string]interface{}{
		"anthropic_version": "bedrock-2023-05-31",
		"max_tokens":        256,
		"temperature":       0.0,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "image",
						"source": map[string]interface{}{
							"type":       "base64",
							"media_type": mimeType,
							"data":       base64.StdEncoding.EncodeToString(imgBytes),
						},
					},
					{
						"type": "text",
						"text": prompt,
					},
				},
			},
		},
	}

	endpoint := "https://bedrock-runtime.us-east-1.amazonaws.com/model/us.anthropic.claude-sonnet-4-6/invoke"
	jsonBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearerToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var claudeResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &claudeResp); err == nil && len(claudeResp.Content) > 0 {
		return strings.TrimSpace(claudeResp.Content[0].Text), nil
	}
	return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
}

func cropImageROI(sourcePath string, bbox []int, refWidth, refHeight int, outPath string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source image: %w", err)
	}
	defer file.Close()

	srcImg, _, err := image.Decode(file)
	if err != nil {
		return fmt.Errorf("decode source image: %w", err)
	}

	bounds := srcImg.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()

	ymin, xmin, ymax, xmax := bbox[0], bbox[1], bbox[2], bbox[3]

	// Auto detect BBox Scale (0-1000 scale vs 0-100 scale vs actual pixel scale)
	maxVal := max(max(ymin, xmin), max(ymax, xmax))

	var cropYmin, cropXmin, cropYmax, cropXmax int
	if maxVal > 100 {
		// 0~1000 normalized integer scale
		cropYmin = int((float64(ymin) / 1000.0) * float64(imgH))
		cropXmin = int((float64(xmin) / 1000.0) * float64(imgW))
		cropYmax = int((float64(ymax) / 1000.0) * float64(imgH))
		cropXmax = int((float64(xmax) / 1000.0) * float64(imgW))
	} else {
		// 0~100 percentage scale
		cropYmin = int((float64(ymin) / 100.0) * float64(imgH))
		cropXmin = int((float64(xmin) / 100.0) * float64(imgW))
		cropYmax = int((float64(ymax) / 100.0) * float64(imgH))
		cropXmax = int((float64(xmax) / 100.0) * float64(imgW))
	}

	// 5% padding around ROI
	padX := int(float64(imgW) * 0.02)
	padY := int(float64(imgH) * 0.02)

	cropXmin = max(0, cropXmin-padX)
	cropYmin = max(0, cropYmin-padY)
	cropXmax = min(imgW, cropXmax+padX)
	cropYmax = min(imgH, cropYmax+padY)

	if cropXmax <= cropXmin || cropYmax <= cropYmin {
		return fmt.Errorf("invalid crop bounds [%d,%d,%d,%d]", cropYmin, cropXmin, cropYmax, cropXmax)
	}

	cropRect := image.Rect(cropXmin, cropYmin, cropXmax, cropYmax)
	subImg, ok := srcImg.(interface {
		SubImage(r image.Rectangle) image.Image
	})
	if !ok {
		return fmt.Errorf("image does not support SubImage")
	}

	cropped := subImg.SubImage(cropRect)

	os.MkdirAll(filepath.Dir(outPath), 0755)
	outFile, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create crop file: %w", err)
	}
	defer outFile.Close()

	return png.Encode(outFile, cropped)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func main() {
	allImages := []string{
		"storage/uploads/photo_2026-08-02_11-36-35.jpg",
		"storage/uploads/photo_2026-08-02_11-37-20.jpg",
		"storage/uploads/photo_2026-08-02_11-37-24.jpg",
		"storage/uploads/photo_2026-08-02_11-37-30.jpg",
		"storage/uploads/photo_2026-08-02_11-37-35.jpg",
		"storage/uploads/photo_2026-08-02_11-37-39.jpg",
	}

	targetImages := allImages
	provider := "bedrock"
	modelID := "us.anthropic.claude-sonnet-4-6"

	if len(os.Args) > 1 {
		if idx, err := strconv.Atoi(os.Args[1]); err == nil && idx >= 1 && idx <= len(allImages) {
			targetImages = []string{allImages[idx-1]}
		}
	}
	if len(os.Args) > 2 {
		provider = os.Args[2]
		if provider == "vertex" {
			modelID = "us.anthropic.claude-sonnet-4-6"
		}
	}

	ctx := context.Background()

	totalPass := 0
	totalWords := 0

	fmt.Println("=========================================================================")
	fmt.Printf("🚀 VOCAT BBOX ROUND-TRIP VERIFICATION SUITE (Testing %d Image(s))\n", len(targetImages))
	fmt.Println("=========================================================================")

	for idx, imgPath := range targetImages {
		imgName := filepath.Base(imgPath)
		fmt.Printf("\n📷 [%d/%d] Processing Image: %s\n", idx+1, len(targetImages), imgName)

		result, err := engine.ConvertOCRToVocatJSON(ctx, "TestRun", true, []string{imgPath}, provider, modelID)
		if err != nil {
			fmt.Printf("❌ Failed to OCR image %s: %v\n", imgName, err)
			continue
		}
		words := result.Words

		fmt.Printf("   Extracted %d main headwords (Title: %q). Running ROI crop & Direct Vision Round-Trip...\n", len(words), result.Title)

		imagePass := 0
		for wIdx, w := range words {
			totalWords++
			cropFilename := fmt.Sprintf("/tmp/roundtrip/img_%s_word%d_%s.png", imgName, w.No, w.Word)

			refW := w.ImageWidth
			refH := w.ImageHeight

			err := cropImageROI(imgPath, w.BBox, refW, refH, cropFilename)
			if err != nil {
				fmt.Printf("   [%2d/%2d] Word: %-18s ❌ Crop Error: %v\n", wIdx+1, len(words), w.Word, err)
				continue
			}

			// Run direct Vision OCR on cropped ROI image
			recText, err := callDirectVisionOCR(ctx, cropFilename)
			found := false

			if err == nil && recText != "" {
				cleanRec := strings.ToLower(recText)
				cleanTarget := strings.ToLower(w.Word)
				if strings.Contains(cleanRec, cleanTarget) || strings.Contains(cleanTarget, cleanRec) {
					found = true
				}
			}

			if found {
				totalPass++
				imagePass++
				fmt.Printf("   [%2d/%2d] Word: %-18s BBox: %v 🟢 PASS (Found: '%s')\n", wIdx+1, len(words), w.Word, w.BBox, recText)
			} else {
				fmt.Printf("   [%2d/%2d] Word: %-18s BBox: %v 🔴 FAIL (ROI Text: '%s') -> ROI: %s\n", wIdx+1, len(words), w.Word, w.BBox, recText, cropFilename)
			}
		}

		fmt.Printf("📊 Image [%s] Accuracy: %d / %d Pass (%.1f%%)\n", imgName, imagePass, len(words), float64(imagePass)/float64(len(words))*100)
	}

	fmt.Println("\n=========================================================================")
	fmt.Printf("🏁 FINAL ROUND-TRIP ACCURACY: %d / %d Passed (%.1f%%)\n", totalPass, totalWords, float64(totalPass)/float64(totalWords)*100)
	fmt.Println("=========================================================================")
}
