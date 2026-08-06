package engine

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func GetTelegramCredentials() (string, string) {
	return LookupConfig("TELEGRAM_BOT_TOKEN"), LookupConfig("TELEGRAM_CHAT_ID")
}

func SendDocToTelegram(docPath, customFileName, caption string) error {
	botToken, chatID := GetTelegramCredentials()
	if botToken == "" || chatID == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN or TELEGRAM_CHAT_ID not configured in environment/.env")
	}

	fullPath := docPath
	if !filepath.IsAbs(fullPath) {
		fullPath = filepath.Join("storage", docPath)
	} else if strings.HasPrefix(fullPath, "/outputs/") {
		// DocPath is stored as web route "/outputs/xxx.doc" — resolve to actual filesystem path
		fullPath = filepath.Join("storage", fullPath)
	}

	fileBytes, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Errorf("failed to read doc file for telegram: %w", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	_ = writer.WriteField("chat_id", chatID)
	if caption != "" {
		_ = writer.WriteField("caption", caption)
	}

	sendFileName := strings.TrimSpace(customFileName)
	if sendFileName == "" {
		sendFileName = filepath.Base(fullPath)
	}
	if !strings.HasSuffix(strings.ToLower(sendFileName), ".doc") {
		sendFileName += ".doc"
	}

	part, err := writer.CreateFormFile("document", sendFileName)
	if err != nil {
		return fmt.Errorf("create form file for telegram: %w", err)
	}
	_, _ = part.Write(fileBytes)
	_ = writer.Close()

	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendDocument", botToken)
	req, err := http.NewRequest("POST", url, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}
