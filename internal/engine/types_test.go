package engine

import (
	"encoding/json"
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
	assert.EqualValues(t, 100, decoded["progress"])
	assert.NotContains(t, decoded, "mu", "the mutex must never appear in output")
	require.Contains(t, decoded, "words")
}
