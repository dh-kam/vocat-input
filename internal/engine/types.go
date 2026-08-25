package engine

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type FlexibleBBox []int

func (b *FlexibleBBox) UnmarshalJSON(data []byte) error {
	var ints []int
	if err := json.Unmarshal(data, &ints); err == nil {
		*b = ints
		return nil
	}
	var floats []float64
	if err := json.Unmarshal(data, &floats); err == nil {
		res := make([]int, len(floats))
		for i, f := range floats {
			res[i] = int(math.Round(f))
		}
		*b = res
		return nil
	}
	*b = nil
	return fmt.Errorf("cannot unmarshal %s into FlexibleBBox", string(data))
}

type WordItem struct {
	No          int          `json:"no"`
	Word        string       `json:"word"`
	Pos         string       `json:"pos"`
	Meaning     interface{}  `json:"meaning"`               // string or []string
	Created     string       `json:"created,omitempty"`     // Timestamp string for sequential ordering
	BBox        FlexibleBBox `json:"bbox,omitempty"`        // [ymin, xmin, ymax, xmax]
	ImageWidth  int          `json:"imageWidth,omitempty"`  // Reference image width used by AI
	ImageHeight int          `json:"imageHeight,omitempty"` // Reference image height used by AI
	ImageIndex  int          `json:"imageIndex,omitempty"`  // 1-indexed
	ImageName   string       `json:"imageName,omitempty"`   // Image filename
}

type StructuringResult struct {
	Title string     `json:"title,omitempty"`
	Words []WordItem `json:"words"`
}

type OCRResult struct {
	ImageIndex  int    `json:"imageIndex"`
	ImageName   string `json:"imageName"`
	ImagePath   string `json:"imagePath"`
	ImageWidth  int    `json:"imageWidth,omitempty"`
	ImageHeight int    `json:"imageHeight,omitempty"`
	RawText     string `json:"rawText"`
	Status      string `json:"status"` // "PENDING", "PROCESSING", "COMPLETED", "FAILED"
	Error       string `json:"error,omitempty"`
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
	deleted       bool         `json:"-"`
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

// MarshalJSON serializes the run while holding its own mutex.
//
// Handlers hand the shared *ConversionRun straight to c.JSON and RunStore.saveToDiskLocked
// marshals every run holding only the store's lock, all while worker goroutines mutate the same
// run through the setters below. Reading Logs or OCRResults while another goroutine appends to
// them is a real data race that -race reports, and the UI polls both endpoints every 350ms
// during a conversion, so it is the normal case rather than an edge one. Locking here rather
// than at each call site means every present and future marshalling path is covered.
func (r *ConversionRun) MarshalJSON() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// plain has no MarshalJSON method, so this does not recurse. The conversion is on the
	// pointer, so the mutex is not copied.
	type plain ConversionRun
	return json.Marshal((*plain)(r))
}

// MarkDeleted marks the run as deleted to prevent zombie saves by background workers.
func (r *ConversionRun) MarkDeleted() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = true
}

// IsDeleted checks if the run was marked deleted.
func (r *ConversionRun) IsDeleted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deleted
}

// TryClaim moves the run into an in-flight status, but only if no pipeline already holds it, and
// reports whether the caller won the claim.
//
// Checking Status and then setting it from a handler is a race with itself. Nothing guarded these
// entry points before, so a double-click on Convert, or the SPA's retry landing next to the
// original request, started two goroutines over the same run: both looped the same Images, both
// called SetWords, and their progress and log updates interleaved into one timeline.
func (r *ConversionRun) TryClaim(status RunStatus) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.deleted || r.Status == RunStatusOCRProgress || r.Status == RunStatusMerging {
		return false
	}
	r.Status = status
	r.Error = ""
	return true
}

// SetTitle exists so handlers stop assigning to run.Title directly and racing the marshaller.
func (r *ConversionRun) SetTitle(title string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Title = title
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

func (r *ConversionRun) SetOCRResultDimensions(i int, width, height int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= 0 && i < len(r.OCRResults) {
		r.OCRResults[i].ImageWidth = width
		r.OCRResults[i].ImageHeight = height
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

// Touch updates UpdatedAt while holding the run's mutex.
func (r *ConversionRun) Touch() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.UpdatedAt = time.Now()
}

func (s *RunStore) loadFromDisk() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.dbFile)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("[RunStore ERROR] Failed to read %s: %v", s.dbFile, err)
		return
	}
	if len(data) == 0 {
		return
	}

	var list []*ConversionRun
	if err := json.Unmarshal(data, &list); err != nil {
		// Atomic isolate corrupted file to prevent infinite backup churn on subsequent restarts
		bakFile := fmt.Sprintf("%s.corrupted.%s", s.dbFile, time.Now().Format("20060102150405"))
		_ = os.Rename(s.dbFile, bakFile)
		log.Printf("[RunStore FATAL] Corrupted %s detected! Isolated to %s. Error: %v", s.dbFile, bakFile, err)
		return
	}

	for _, run := range list {
		s.runs[run.ID] = run
	}
}

func (s *RunStore) saveToDiskLocked() {
	var list []*ConversionRun
	for _, r := range s.runs {
		if !r.IsDeleted() {
			list = append(list, r)
		}
	}
	// Sort by CreatedAt desc
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})

	bytesData, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		log.Printf("[RunStore ERROR] Failed to marshal runs: %v", err)
		return
	}

	// Atomic write: write to temp file then rename with sync
	tmpFile := fmt.Sprintf("%s.tmp.%d", s.dbFile, time.Now().UnixNano())
	f, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		log.Printf("[RunStore ERROR] Failed to create tmp db file: %v", err)
		return
	}

	if _, err := f.Write(bytesData); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		log.Printf("[RunStore ERROR] Failed to write tmp db file: %v", err)
		return
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpFile)
		log.Printf("[RunStore ERROR] Failed to sync tmp db file: %v", err)
		return
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpFile)
		log.Printf("[RunStore ERROR] Failed to close tmp db file: %v", err)
		return
	}

	if err := os.Rename(tmpFile, s.dbFile); err != nil {
		_ = os.Remove(tmpFile)
		log.Printf("[RunStore ERROR] Failed to rename tmp db file to %s: %v", s.dbFile, err)
	}
}

func (s *RunStore) Save(run *ConversionRun) {
	if run == nil || run.IsDeleted() {
		return
	}
	run.Touch()
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.IsDeleted() {
		return
	}
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
		if !r.IsDeleted() {
			list = append(list, r)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	return list
}

func (s *RunStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok {
		r.MarkDeleted()
	}
	delete(s.runs, id)
	s.saveToDiskLocked()
}
