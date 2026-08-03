package main

import (
	"context"
	"fmt"
	"os"

	"vocat-input/internal/engine"
)

func main() {
	imagePaths := []string{
		"storage/uploads/photo_2026-08-02_11-36-35.jpg",
	}

	for _, p := range imagePaths {
		if _, err := os.Stat(p); err != nil {
			fmt.Printf("Image file not found: %s\n", p)
			return
		}
	}

	ctx := context.Background()

	fmt.Println("=== RUNNING CLEAN VERIFICATION CONVERSION ===")
	words, err := engine.ConvertOCRToVocatJSON(ctx, "TestClean", true, imagePaths, "bedrock", "us.anthropic.claude-sonnet-4-6")
	if err != nil {
		fmt.Printf("❌ Conversion failed: %v\n", err)
		return
	}

	fmt.Printf("\n✅ EXTRACTED TOTAL %d WORDS:\n", len(words))
	for idx, w := range words {
		fmt.Printf("%2d | No: %2d | Word: %-20s | POS: %s | Meaning: %s\n", idx+1, w.No, w.Word, w.Pos, w.Meaning)
	}


}
