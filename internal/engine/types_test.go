package engine

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Run under -race, this is the regression test for the marshalling race: handlers used to hand
// the shared *ConversionRun to c.JSON, and RunStore.saveToDiskLocked marshalled every run holding
// only the store's lock, while worker goroutines appended to Logs and OCRResults through the
// setters. Without ConversionRun.MarshalJSON taking the run's own mutex the detector fires here.
func TestConversionRun_MarshalIsSafeWhileMutating(t *testing.T) {
	run := &ConversionRun{
		ID:         "run_race",
		Status:     RunStatusOCRProgress,
		OCRResults: []*OCRResult{{ImageIndex: 1, ImageName: "page.jpg", Status: "PENDING"}},
	}

	var wg sync.WaitGroup

	// The worker: exactly what the convert goroutine does per image.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 500 {
			run.AddLog("processing page")
			run.SetProgress(i % 100)
			run.SetOCRResultStatus(0, "PROCESSING")
			run.SetOCRResultText(0, "1. apple 사과")
			run.SetWords([]WordItem{{No: 1, Word: "apple", Meaning: "사과"}})
			run.SetTitle("run title")
		}
	}()

	// Two readers: the detail endpoint and the full-store persistence pass.
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				_, err := json.Marshal(run)
				require.NoError(t, err)
			}
		}()
	}

	wg.Wait()
}

// The store marshals every run it holds, so a concurrent run being mutated must not race with a
// different run being saved.
func TestRunStore_SaveIsSafeWhileAnotherRunMutates(t *testing.T) {
	store := NewRunStore(t.TempDir())

	quiet := &ConversionRun{ID: "run_quiet", Status: RunStatusCompleted}
	busy := &ConversionRun{ID: "run_busy", Status: RunStatusOCRProgress}
	store.Save(quiet)
	store.Save(busy)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 300 {
			busy.AddLog("still working")
			busy.SetProgress(i % 100)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 300 {
			store.Save(quiet) // marshals every run, including busy
		}
	}()

	wg.Wait()
}

// Marshalling must still produce the same shape it always did; the lock is the only change.
func TestConversionRun_MarshalShape(t *testing.T) {
	run := &ConversionRun{
		ID:       "run_1",
		Title:    "Vocat Run 08-06 09:26",
		Status:   RunStatusCompleted,
		Progress: 100,
		Words:    []WordItem{{No: 1, Word: "apple", Pos: "명", Meaning: "사과"}},
	}

	data, err := json.Marshal(run)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "run_1", decoded["id"])
	assert.Equal(t, "Vocat Run 08-06 09:26", decoded["title"])
	assert.Equal(t, "COMPLETED", decoded["status"])
	assert.Equal(t, float64(100), decoded["progress"])
	assert.NotContains(t, decoded, "mu", "the mutex must never appear in output")
	require.Contains(t, decoded, "words")
}

func TestRunStore_DeletedRunProtection(t *testing.T) {
	dir := t.TempDir()
	store := NewRunStore(dir)

	run := &ConversionRun{ID: "run_delete_test", Status: RunStatusOCRProgress}
	store.Save(run)

	// Delete the run
	store.Delete("run_delete_test")
	assert.True(t, run.IsDeleted())

	_, exists := store.Get("run_delete_test")
	assert.False(t, exists)

	// Late save from background worker should be ignored
	store.Save(run)
	_, exists = store.Get("run_delete_test")
	assert.False(t, exists)
}

func TestRunStore_AtomicSaveAndCorruptedBackup(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/runs_db.json"

	// Write corrupted JSON
	require.NoError(t, os.WriteFile(dbPath, []byte(`[{"id": "broken"`), 0644))

	// Init store with corrupted file
	store := NewRunStore(dir)
	assert.Empty(t, store.List())

	// Check that backup file was created
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	hasBackup := false
	for _, f := range files {
		if strings.Contains(f.Name(), "runs_db.json.corrupted.") {
			hasBackup = true
			break
		}
	}
	assert.True(t, hasBackup, "Backup file should be created for corrupted db")
}
