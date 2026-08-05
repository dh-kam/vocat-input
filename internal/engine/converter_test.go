package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vocat-input/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type SourceWordsWrapper struct {
	Words []engine.WordItem `json:"words"`
}

func parseInputJSON(data []byte) ([]engine.WordItem, error) {
	var items []engine.WordItem
	if err := json.Unmarshal(data, &items); err == nil && len(items) > 0 {
		return items, nil
	}
	var wrapper SourceWordsWrapper
	if err := json.Unmarshal(data, &wrapper); err == nil && len(wrapper.Words) > 0 {
		return wrapper.Words, nil
	}
	return nil, json.Unmarshal(data, &items)
}

func TestGenerateDocFile_Regression(t *testing.T) {
	testDir := filepath.Join("testdata", "regression")
	
	files, err := os.ReadDir(testDir)
	require.NoError(t, err, "Failed to read test directory")

	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		
		baseName := strings.TrimSuffix(file.Name(), ".json")
		jsonPath := filepath.Join(testDir, file.Name())
		docPath := filepath.Join(testDir, baseName+".doc")

		t.Run(baseName, func(t *testing.T) {
			jsonData, err := os.ReadFile(jsonPath)
			require.NoError(t, err)

			words, err := parseInputJSON(jsonData)
			require.NoError(t, err)
			require.NotEmpty(t, words)

			tempDocPath := filepath.Join(t.TempDir(), baseName+".doc")
			err = engine.GenerateDocFile(words, tempDocPath)
			require.NoError(t, err)

			generatedData, err := os.ReadFile(tempDocPath)
			require.NoError(t, err)

			expectedData, err := os.ReadFile(docPath)
			require.NoError(t, err)

			var genBook, expBook engine.VocatBook
			err = json.Unmarshal(generatedData, &genBook)
			require.NoError(t, err, "Generated DOC is not valid JSON VocatBook")
			
			err = json.Unmarshal(expectedData, &expBook)
			require.NoError(t, err, "Expected DOC is not valid JSON VocatBook")

			assert.Equal(t, len(expBook.CorpusList), len(genBook.CorpusList), "Corpus count mismatch")
			
			// Compare some core values ignoring random IDs and timestamps
			for i := range expBook.CorpusList {
				expW := expBook.CorpusList[i]["word"]
				genW := genBook.CorpusList[i]["word"]
				assert.Equal(t, expW, genW, "Word mismatch at index %d", i)

				expM := expBook.CorpusList[i]["meaning"]
				genM := genBook.CorpusList[i]["meaning"]
				assert.Equal(t, expM, genM, "Meaning mismatch at index %d", i)
				
				expP := expBook.CorpusList[i]["pos"]
				genP := genBook.CorpusList[i]["pos"]
				assert.Equal(t, expP, genP, "POS mismatch at index %d", i)
			}
		})
	}
}
