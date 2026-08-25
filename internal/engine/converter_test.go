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

func TestCleanTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "  **절대수능맵 VOCA Inter 3 Vocabulary Test(기본) DAY 01**  ",
			want:  "절대수능맵 VOCA Inter 3 Vocabulary Test(기본) DAY 01",
		},
		{
			input: "Title: 워드마스터 수능 2000 DAY 15",
			want:  "워드마스터 수능 2000 DAY 15",
		},
		{
			input: "교재명: `EBS 수능특강 영어 Day 03`",
			want:  "EBS 수능특강 영어 Day 03",
		},
		{
			input: "단어장",
			want:  "", // Generic name filtered out
		},
		{
			input: "null",
			want:  "",
		},
		{
			input: "none",
			want:  "",
		},
		{
			input: "A",
			want:  "", // Too short
		},
		{
			input: "\"Hackers TOEIC Voca Day 01 (기초)\"",
			want:  "Hackers TOEIC Voca Day 01 (기초)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := engine.CleanTitle(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "절대수능맵 VOCA Inter 3 / DAY 01: Test",
			want:  "절대수능맵 VOCA Inter 3 _ DAY 01_ Test",
		},
		{
			input: "Word<Master>*?|Doc",
			want:  "Word_Master____Doc",
		},
		{
			input: "   ",
			want:  "Vocat_Material",
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := engine.SanitizeFileName(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestGenerateDocFile_CustomTitle(t *testing.T) {
	words := []engine.WordItem{
		{No: 1, Word: "apple", Pos: "명", Meaning: "사과"},
	}
	customTitle := "절대수능맵 VOCA Inter 3 DAY 01 (기본)"
	tempDocPath := filepath.Join(t.TempDir(), "test.doc")

	err := engine.GenerateDocFile(words, tempDocPath, customTitle)
	require.NoError(t, err)

	data, err := os.ReadFile(tempDocPath)
	require.NoError(t, err)

	var book engine.VocatBook
	err = json.Unmarshal(data, &book)
	require.NoError(t, err)

	assert.Equal(t, customTitle, book.Vocabulary["name"])
	assert.EqualValues(t, 1, book.Vocabulary["total"])
}
