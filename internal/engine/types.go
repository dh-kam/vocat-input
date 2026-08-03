package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type WordItem struct {
	No          int         `json:"no"`
	Word        string      `json:"word"`
	Pos         string      `json:"pos"`
	Meaning     interface{} `json:"meaning"`             // string or []string
	Created     string      `json:"created,omitempty"`   // Timestamp string for sequential ordering
	BBox        []int       `json:"bbox,omitempty"`      // [ymin, xmin, ymax, xmax]
	ImageWidth  int         `json:"imageWidth,omitempty"`  // Reference image width used by AI
	ImageHeight int         `json:"imageHeight,omitempty"` // Reference image height used by AI
	ImageIndex  int         `json:"imageIndex,omitempty"`// 1-indexed
	ImageName   string      `json:"imageName,omitempty"` // Image filename
}

type OCRResult struct {
	ImageIndex int    `json:"imageIndex"`
	ImageName  string `json:"imageName"`
	ImagePath  string `json:"imagePath"`
	RawText    string `json:"rawText"`
	Status     string `json:"status"` // "PENDING", "PROCESSING", "COMPLETED", "FAILED"
	Error      string `json:"error,omitempty"`
}

type TransformationRule struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	PosMapping map[string]string `json:"posMapping"`
}

type RunStatus string

const (
	RunStatusCreated     RunStatus = "CREATED"
	RunStatusOCRProgress RunStatus = "OCR_IN_PROGRESS"
	RunStatusOCRDone     RunStatus = "OCR_COMPLETED"
	RunStatusMerging     RunStatus = "MERGING_CONVERTING"
	RunStatusCompleted   RunStatus = "COMPLETED"
	RunStatusFailed      RunStatus = "FAILED"
)

type ConversionRun struct {
	mu            sync.Mutex   `json:"-"`
	ID            string       `json:"id"`
	Title         string       `json:"title"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
	Status        RunStatus    `json:"status"`
	Progress      int          `json:"progress"` // 0 to 100
	OCRProvider   string       `json:"ocrProvider"`
	OCRModel      string       `json:"ocrModel,omitempty"`
	BBoxScale     int          `json:"bboxScale"` // 100 or 1000
	ProvidersList []string     `json:"providersList,omitempty"`
	PreserveOrder bool         `json:"preserveOrder"`
	RuleID        string       `json:"ruleId"`
	Images        []string     `json:"images"`
	OCRResults    []*OCRResult `json:"ocrResults"`
	Logs          []string     `json:"logs,omitempty"`
	MergedText    string       `json:"mergedText,omitempty"`
	Words         []WordItem   `json:"words,omitempty"`
	JSONPath      string       `json:"jsonPath,omitempty"`
	DocPath       string       `json:"docPath,omitempty"`
	Error         string       `json:"error,omitempty"`
}

func GetModelBBoxScale(provider, model string) int {
	p := strings.ToLower(provider)
	m := strings.ToLower(model)

	if p == "vertex" || p == "gemini" || strings.Contains(m, "gemini") {
		return 1000
	}
	if p == "bedrock" || strings.Contains(m, "nova") || strings.Contains(m, "claude") {
		return 100
	}
	return 1000
}

func (r *ConversionRun) AddLog(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg)
	r.Logs = append(r.Logs, entry)
}

func (r *ConversionRun) SetProgress(val int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if val < 0 {
		val = 0
	}
	if val > 100 {
		val = 100
	}
	r.Progress = val
}

func (r *ConversionRun) SetStatus(s RunStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Status = s
}

func (r *ConversionRun) SetOCRResultStatus(i int, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= 0 && i < len(r.OCRResults) {
		r.OCRResults[i].Status = status
	}
}

func (r *ConversionRun) SetOCRResultError(i int, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= 0 && i < len(r.OCRResults) {
		r.OCRResults[i].Error = errMsg
	}
}

func (r *ConversionRun) SetOCRResultText(i int, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= 0 && i < len(r.OCRResults) {
		r.OCRResults[i].RawText = text
	}
}

func (r *ConversionRun) SetMergedText(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.MergedText = text
}

func (r *ConversionRun) SetWords(words []WordItem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Words = words
}

func (r *ConversionRun) SetOutputPaths(jsonPath, docPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.JSONPath = jsonPath
	r.DocPath = docPath
}

func (r *ConversionRun) SetDocPath(docPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.DocPath = docPath
}

func (r *ConversionRun) SetError(errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Error = errMsg
}

type RunStore struct {
	mu         sync.RWMutex
	storageDir string
	dbFile     string
	runs       map[string]*ConversionRun
}

func NewRunStore(storageDir string) *RunStore {
	if storageDir == "" {
		storageDir = "./storage"
	}
	_ = os.MkdirAll(storageDir, 0755)
	dbFile := filepath.Join(storageDir, "runs_db.json")

	store := &RunStore{
		storageDir: storageDir,
		dbFile:     dbFile,
		runs:       make(map[string]*ConversionRun),
	}
	store.loadFromDisk()
	return store
}

func (s *RunStore) loadFromDisk() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.dbFile)
	if err != nil || len(data) == 0 {
		return
	}

	var list []*ConversionRun
	if err := json.Unmarshal(data, &list); err == nil {
		for _, run := range list {
			s.runs[run.ID] = run
		}
	}
}

func (s *RunStore) saveToDiskLocked() {
	var list []*ConversionRun
	for _, r := range s.runs {
		list = append(list, r)
	}
	// Sort by CreatedAt desc
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})

	bytesData, err := json.MarshalIndent(list, "", "  ")
	if err == nil {
		_ = os.WriteFile(s.dbFile, bytesData, 0644)
	}
}

func (s *RunStore) Save(run *ConversionRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run.UpdatedAt = time.Now()
	s.runs[run.ID] = run
	s.saveToDiskLocked()
}

func (s *RunStore) Get(id string) (*ConversionRun, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	return run, ok
}

func (s *RunStore) List() []*ConversionRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []*ConversionRun
	for _, r := range s.runs {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	return list
}

func (s *RunStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runs, id)
	s.saveToDiskLocked()
}
