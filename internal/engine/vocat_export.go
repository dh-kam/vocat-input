package engine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type VocatBook struct {
	Vocabulary map[string]interface{}   `json:"vocabulary"`
	CorpusList []map[string]interface{} `json:"corpusList"`
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomID() (string, error) {
	a, err := randomHex(2)
	if err != nil {
		return "", err
	}
	b, err := randomHex(2)
	if err != nil {
		return "", err
	}
	c, err := randomHex(4)
	if err != nil {
		return "", err
	}
	d, err := randomHex(2)
	if err != nil {
		return "", err
	}
	e, err := randomHex(6)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%s-%s-%s", a, b, c, d, e), nil
}

func nowZ() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05.000000Z")
}

func convertPos(pos string) string {
	p := strings.ToLower(strings.TrimSpace(pos))
	switch p {
	case "adjective", "형용사":
		return "형"
	case "noun", "명사":
		return "명"
	case "verb", "동사":
		return "동"
	case "adverb", "부사":
		return "부"
	case "preposition", "전치사":
		return "전"
	case "conjunction", "접속사":
		return "접"
	case "article", "관사":
		return "관"
	case "interjection", "감탄사":
		return "감"
	default:
		return strings.TrimSpace(pos)
	}
}

func inferPos(word, meaning, posRaw string) string {
	if p := strings.TrimSpace(posRaw); p != "" {
		return p
	}
	w := strings.ToLower(strings.TrimSpace(word))
	m := strings.TrimSpace(meaning)
	if strings.HasSuffix(w, "ly") || strings.Contains(m, "하게") {
		return "부"
	}
	if strings.HasSuffix(w, "tion") || strings.HasSuffix(w, "sion") || strings.HasSuffix(w, "ment") || strings.HasSuffix(w, "ness") ||
		strings.Contains(m, "것") || strings.Contains(m, "상태") || strings.Contains(m, "학") {
		return "명"
	}
	if strings.HasSuffix(w, "ive") || strings.HasSuffix(w, "ous") || strings.HasSuffix(w, "al") || strings.HasSuffix(w, "able") ||
		strings.Contains(m, "한") || strings.Contains(m, "적인") {
		return "형"
	}
	if strings.HasSuffix(w, "ate") || strings.HasSuffix(w, "fy") || strings.HasSuffix(w, "ize") ||
		strings.Contains(m, "하다") || strings.Contains(m, "시키다") || strings.Contains(m, "되다") {
		return "동"
	}
	return "명"
}

func normalizeMeaning(v interface{}) string {
	switch mv := v.(type) {
	case string:
		parts := splitMeaningByCommaOutsideParens(mv)
		return strings.Join(parts, "﹒")
	case []interface{}:
		out := make([]string, 0, len(mv))
		for _, item := range mv {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s == "" {
				continue
			}
			for _, p := range splitMeaningByCommaOutsideParens(s) {
				if p != "" {
					out = append(out, p)
				}
			}
		}
		return strings.Join(out, "﹒")
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", mv))
		if s == "" {
			return ""
		}
		return strings.Join(splitMeaningByCommaOutsideParens(s), "﹒")
	}
}

func splitMeaningByCommaOutsideParens(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(':
			depth++
			b.WriteRune(r)
		case ')':
			if depth > 0 {
				depth--
			}
			b.WriteRune(r)
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(b.String())
				if part != "" {
					out = append(out, part)
				}
				b.Reset()
			} else {
				b.WriteRune(r)
			}
		default:
			b.WriteRune(r)
		}
	}
	last := strings.TrimSpace(b.String())
	if last != "" {
		out = append(out, last)
	}
	if len(out) == 0 {
		return []string{s}
	}
	return out
}

func GenerateDocFile(words []WordItem, outputPath string, customTitle ...string) error {
	if len(words) == 0 {
		return fmt.Errorf("GenerateDocFile: empty words list")
	}

	vocabID, err := randomID()
	if err != nil {
		return fmt.Errorf("failed to generate vocab ID: %w", err)
	}
	bookcaseID, err := randomID()
	if err != nil {
		return fmt.Errorf("failed to generate bookcase ID: %w", err)
	}

	ts := nowZ()
	name := strings.TrimSuffix(filepath.Base(outputPath), filepath.Ext(outputPath))
	if len(customTitle) > 0 && strings.TrimSpace(customTitle[0]) != "" {
		name = strings.TrimSpace(customTitle[0])
	}

	corpus := make([]map[string]interface{}, 0, len(words))
	for i, it := range words {
		if strings.TrimSpace(it.Word) == "" {
			return fmt.Errorf("GenerateDocFile: empty word at index %d", i)
		}
		meaning := normalizeMeaning(it.Meaning)
		if meaning == "" || meaning == "<nil>" {
			return fmt.Errorf("GenerateDocFile: empty meaning at index %d (word: %s)", i, it.Word)
		}
		
		posRaw := strings.TrimSpace(it.Pos)
		if posRaw == "" {
			posRaw = inferPos(it.Word, meaning, posRaw)
		}

		id, idErr := randomID()
		if idErr != nil {
			return fmt.Errorf("failed to generate corpus ID: %w", idErr)
		}
		
		itemTS := nowZ()
		corpus = append(corpus, map[string]interface{}{
			"id":            id,
			"vocabularyId":  vocabID,
			"word":          it.Word,
			"meaning":       meaning,
			"pos":           convertPos(posRaw),
			"pronunciation": nil,
			"synonym":       nil,
			"antonym":       nil,
			"desc":          nil,
			"image":         nil,
			"familiar":      0,
			"scheduledAt":   itemTS,
			"updatedAt":     itemTS,
			"createdAt":     itemTS,
		})
	}

	book := VocatBook{
		Vocabulary: map[string]interface{}{
			"id":             vocabID,
			"bookcaseId":     bookcaseID,
			"name":           name,
			"desc":           "",
			"wordLang":       "englishUs",
			"meaningLang":    "korean",
			"total":          len(corpus),
			"nFamiliar":      0,
			"nUnfamiliar":    0,
			"price":          0,
			"isShowSchedule": 1,
			"isSharable":     1,
			"updatedAt":      ts,
			"createdAt":      ts,
		},
		CorpusList: corpus,
	}

	out, err := json.Marshal(book)
	if err != nil {
		return fmt.Errorf("failed to marshal VocatBook JSON: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outputPath, out, 0o644)
}
