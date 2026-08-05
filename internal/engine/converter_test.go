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

func TestGenerateDocFile_Regression(t *testing.T) {
	testDir := filepath.Join("testdata", "regression")
	
	// Find all .json files in testdata/regression
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
			// Read JSON input
			jsonData, err := os.ReadFile(jsonPath)
			require.NoError(t, err, "Failed to read json file")

			var words []engine.WordItem
			err = json.Unmarshal(jsonData, &words)
			require.NoError(t, err, "Failed to parse json file")

			// Generate output to a temporary file
			tempDocPath := filepath.Join(t.TempDir(), baseName+".doc")
			err = engine.GenerateDocFile(words, tempDocPath)
			require.NoError(t, err, "Failed to generate doc file")

			// Read generated output
			generatedData, err := os.ReadFile(tempDocPath)
			require.NoError(t, err, "Failed to read generated doc file")

			// Read expected output
			expectedData, err := os.ReadFile(docPath)
			require.NoError(t, err, "Failed to read expected doc file")

			// Compare string contents
			assert.Equal(t, string(expectedData), string(generatedData), "Generated doc file does not match expected output for %s", baseName)
		})
	}
}
