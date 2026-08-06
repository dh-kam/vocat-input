package engine

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bboxInvariants is the contract every batch leaving cleanWordItems must satisfy. The
// randomized and fuzz tests below both check it, so a future change to the normalization has
// to keep all of it true rather than just the hand-written cases.
func bboxInvariants(t *testing.T, label string, out []WordItem) {
	t.Helper()
	for _, w := range out {
		require.Len(t, w.BBox, 4, "%s/%s: box must be exactly 4 coordinates, got %v", label, w.Word, w.BBox)
		for _, c := range w.BBox {
			assert.GreaterOrEqual(t, c, 0, "%s/%s: coordinate below 0 in %v", label, w.Word, w.BBox)
			assert.LessOrEqual(t, c, BBoxOutputScale, "%s/%s: coordinate above the output scale in %v", label, w.Word, w.BBox)
		}
		assert.Greater(t, w.BBox[2], w.BBox[0], "%s/%s: box has no height: %v", label, w.Word, w.BBox)
		assert.Greater(t, w.BBox[3], w.BBox[1], "%s/%s: box has no width: %v", label, w.Word, w.BBox)
	}
}

func words(boxes ...[]int) []WordItem {
	items := make([]WordItem, 0, len(boxes))
	for i, b := range boxes {
		items = append(items, WordItem{
			No:      i + 1,
			Word:    []string{"alpha", "bravo", "charlie", "delta", "echo"}[i%5],
			Pos:     "명",
			Meaning: "뜻",
			BBox:    b,
		})
	}
	return items
}

func boxesOf(items []WordItem) [][]int {
	out := make([][]int, 0, len(items))
	for _, w := range items {
		out = append(out, w.BBox)
	}
	return out
}

// Every box leaving cleanWordItems must be on the 0-100 scale. The corruption happened
// because placeholders were emitted on the 0-1000 scale while real boxes were normalized to
// 0-100, leaving the batch on two scales at once.
func TestCleanWordItems_OutputIsAlwaysPercentageScale(t *testing.T) {
	cases := map[string][][]int{
		"0-1000 input":       {{120, 50, 260, 950}, {300, 50, 440, 950}},
		"already percentage": {{12, 5, 26, 95}, {30, 5, 44, 95}},
		"mixed with missing": {{120, 50, 260, 950}, nil, {300, 50, 440, 950}},
		"all missing":        {nil, nil},
	}

	for name, boxes := range cases {
		t.Run(name, func(t *testing.T) {
			for _, w := range cleanWordItems(words(boxes...)) {
				require.Len(t, w.BBox, 4, "%s: every item must end up with a 4-element box", w.Word)
				for _, v := range w.BBox {
					assert.GreaterOrEqual(t, v, 0, "%s: coordinate below 0", w.Word)
					assert.LessOrEqual(t, v, BBoxOutputScale, "%s: coordinate above the output scale", w.Word)
				}
				assert.Greater(t, w.BBox[2], w.BBox[0], "%s: box has no height", w.Word)
				assert.Greater(t, w.BBox[3], w.BBox[1], "%s: box has no width", w.Word)
			}
		})
	}
}

// The actual regression. run_1785714340533 was produced by two passes over the same items:
// parseStructuredJSONResponse cleaned them, then ConvertOCRToVocatJSON cleaned the result
// again, dividing every already-normalized coordinate by 10 a second time.
func TestCleanWordItems_IsIdempotent(t *testing.T) {
	cases := map[string][][]int{
		"all boxes valid":            {{120, 50, 260, 950}, {300, 50, 440, 950}},
		"one box missing":            {{120, 50, 260, 950}, nil, {300, 50, 440, 950}},
		"one box degenerate":         {{120, 50, 260, 950}, {400, 50, 400, 950}},
		"one box inverted":           {{120, 50, 260, 950}, {500, 950, 400, 50}},
		"one box all zeroes":         {{120, 50, 260, 950}, {0, 0, 0, 0}},
		"boxes near top-left corner": {{5, 20, 9, 30}, {12, 20, 18, 30}},
		// A trailing fifth element used to keep its original magnitude through
		// normalization, then re-trigger scale detection on the next pass.
		"box with a trailing element": {{100, 50, 300, 950, 777}},
		"coordinates at the bounds":   {{0, 0, 100, 100}, {10, 5, 100, 95}},
	}

	for name, boxes := range cases {
		t.Run(name, func(t *testing.T) {
			once := cleanWordItems(words(boxes...))
			twice := cleanWordItems(once)
			assert.Equal(t, boxesOf(once), boxesOf(twice), "second pass changed the boxes")
		})
	}
}

// cleanWordItems must not rescale the slice it was handed, or the damage outlives the call.
func TestCleanWordItems_DoesNotMutateInput(t *testing.T) {
	input := words([]int{120, 50, 260, 950})
	cleanWordItems(input)
	assert.Equal(t, []int{120, 50, 260, 950}, input[0].BBox, "input box was rewritten in place")
}

// A 0-1000 box only 4 units tall rounds to 0% of height. It must not be flattened into a
// degenerate box, because the caller would then replace it with a placeholder pointing
// somewhere else on the page. The second box puts the batch on the 0-1000 scale so the
// rounding path actually runs.
func TestNormalizeBBoxes_ThinBoxSurvivesRounding(t *testing.T) {
	items := words([]int{5, 20, 9, 30}, []int{600, 50, 900, 950})
	normalizeBBoxes(items)

	assert.Greater(t, items[0].BBox[2], items[0].BBox[0], "thin box lost its height")
	assert.Greater(t, items[0].BBox[3], items[0].BBox[1], "thin box lost its width")
	assert.Equal(t, []int{60, 5, 90, 95}, items[1].BBox, "the normal box should be unaffected")
}

// A box at the very bottom edge cannot be widened downwards, so the top edge has to give way
// instead of the span staying degenerate.
func TestNormalizeBBoxes_ThinBoxAtBottomEdge(t *testing.T) {
	items := words([]int{998, 998, 999, 999}, []int{100, 50, 200, 950})
	normalizeBBoxes(items)

	assert.Greater(t, items[0].BBox[2], items[0].BBox[0], "bottom-edge box lost its height")
	assert.Greater(t, items[0].BBox[3], items[0].BBox[1], "bottom-edge box lost its width")
	assert.LessOrEqual(t, items[0].BBox[2], BBoxOutputScale)
	assert.LessOrEqual(t, items[0].BBox[3], BBoxOutputScale)
}

func TestNormalizeBBoxes_ScaleDetection(t *testing.T) {
	t.Run("0-1000 divides by ten", func(t *testing.T) {
		items := words([]int{120, 50, 260, 950})
		normalizeBBoxes(items)
		assert.Equal(t, []int{12, 5, 26, 95}, items[0].BBox)
	})

	t.Run("percentages are left alone", func(t *testing.T) {
		items := words([]int{12, 5, 26, 95})
		normalizeBBoxes(items)
		assert.Equal(t, []int{12, 5, 26, 95}, items[0].BBox)
	})

	t.Run("pixels use each axis image dimension", func(t *testing.T) {
		items := words([]int{300, 80, 600, 1520})
		items[0].ImageHeight = 3000
		items[0].ImageWidth = 1600
		normalizeBBoxes(items)
		assert.Equal(t, []int{10, 5, 20, 95}, items[0].BBox)
	})

	t.Run("pixels without a reference frame fall back to the batch maximum", func(t *testing.T) {
		items := words([]int{300, 80, 600, 1520})
		normalizeBBoxes(items)
		for _, v := range items[0].BBox {
			assert.LessOrEqual(t, v, BBoxOutputScale)
		}
		assert.Greater(t, items[0].BBox[2], items[0].BBox[0])
	})
}

// ImageWidth/ImageHeight are the reference frame for a pixel-scale box, so dropping them
// during the rebuild made pixel coordinates uninterpretable downstream.
func TestCleanWordItems_KeepsImageDimensions(t *testing.T) {
	input := words([]int{12, 5, 26, 95})
	input[0].ImageWidth = 1600
	input[0].ImageHeight = 3000

	out := cleanWordItems(input)
	require.Len(t, out, 1)
	assert.Equal(t, 1600, out[0].ImageWidth)
	assert.Equal(t, 3000, out[0].ImageHeight)
}

// Garbage a model can emit must never reach storage as-is. A box whose coordinates are all
// negative is the case that slipped through an earlier version of this fix: the batch maximum
// stayed at 0, so no rescaling ran, and the box was still an ordered rectangle so it passed
// the validity check unchanged.
func TestCleanWordItems_RejectsOutOfRangeBoxes(t *testing.T) {
	cases := map[string][]int{
		"all negative":     {-4, -3, -2, -1},
		"beyond the scale": {200, 300, 400, 500},
		"too short":        {5, 10},
	}

	for name, box := range cases {
		t.Run(name, func(t *testing.T) {
			out := cleanWordItems(words(box))
			require.Len(t, out, 1)
			assert.True(t, bboxIsUsable(out[0].BBox), "unusable box escaped: %v", out[0].BBox)
			assert.Len(t, out[0].BBox, 4, "box should be exactly 4 coordinates")
		})
	}
}

// The placeholders exist so every row stays selectable; they should spread down the page
// rather than stack on one line.
func TestCleanWordItems_PlaceholdersAreDistinct(t *testing.T) {
	out := cleanWordItems(words(nil, nil, nil, nil))
	require.Len(t, out, 4)

	seen := map[int]bool{}
	for _, w := range out {
		assert.False(t, seen[w.BBox[0]], "two placeholders share a top edge")
		seen[w.BBox[0]] = true
	}
}

// randomBatch produces a batch shaped like real model output: mostly well-formed 0-1000 row
// boxes marching down the page, plus the share of failures models actually emit.
func randomBatch(rng *rand.Rand) []WordItem {
	n := 5 + rng.Intn(40)
	items := make([]WordItem, 0, n)
	rowH := 1000 / n

	for i := range n {
		w := WordItem{
			No: i + 1, Word: fmt.Sprintf("word%03d", i), Pos: "명", Meaning: "뜻",
			ImageWidth: 1600, ImageHeight: 2400,
		}
		top := i * rowH
		switch roll := rng.Intn(100); {
		case roll < 70: // healthy row box
			w.BBox = []int{top, 20 + rng.Intn(30), top + rowH - 2, 950 + rng.Intn(50)}
		case roll < 78: // absent
			w.BBox = nil
		case roll < 84: // all zero
			w.BBox = []int{0, 0, 0, 0}
		case roll < 89: // no height
			w.BBox = []int{top, 20, top, 950}
		case roll < 93: // inverted
			w.BBox = []int{top + rowH, 950, top, 20}
		case roll < 96: // thin but real
			w.BBox = []int{top, 20, top + 1 + rng.Intn(3), 950}
		case roll < 98: // truncated
			w.BBox = []int{top, 20}
		default: // stray trailing element
			w.BBox = []int{top, 20, top + rowH - 2, 950, rng.Intn(5000)}
		}
		items = append(items, w)
	}
	return items
}

func snapshotBoxes(items []WordItem) [][]int {
	out := make([][]int, len(items))
	for i, w := range items {
		out[i] = append([]int(nil), w.BBox...)
	}
	return out
}

// The property test that the hand-written cases cannot cover: thousands of randomized batches,
// each checked for the output invariants, idempotence, and leaving the caller's slice alone.
// Seeds are fixed so a failure is reproducible.
func TestCleanWordItems_RandomizedProperties(t *testing.T) {
	const rounds, perRound = 20, 250

	for round := 1; round <= rounds; round++ {
		rng := rand.New(rand.NewSource(int64(round)))
		for c := range perRound {
			input := randomBatch(rng)
			before := snapshotBoxes(input)

			once := cleanWordItems(input)
			bboxInvariants(t, fmt.Sprintf("round %d case %d", round, c), once)

			twice := cleanWordItems(once)
			if !reflect.DeepEqual(snapshotBoxes(once), snapshotBoxes(twice)) {
				t.Fatalf("round %d case %d: not idempotent\n  once:  %v\n  twice: %v",
					round, c, snapshotBoxes(once), snapshotBoxes(twice))
			}
			if !reflect.DeepEqual(before, snapshotBoxes(input)) {
				t.Fatalf("round %d case %d: rewrote the caller's boxes", round, c)
			}
			if t.Failed() {
				t.FailNow()
			}
		}
	}
}

// FuzzCleanWordItems searches for any input that breaks the output invariants or idempotence.
func FuzzCleanWordItems(f *testing.F) {
	f.Add([]byte{120, 50, 4, 200, 0, 0, 0, 0})
	f.Add([]byte{5, 20, 9, 30})
	f.Add([]byte{255, 255, 255, 255, 255})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 || len(data) > 4096 {
			return
		}
		var items []WordItem
		for i := 0; i+1 < len(data); i += 5 {
			end := min(i+4, len(data))
			bbox := make([]int, 0, 4)
			for _, b := range data[i:end] {
				// Spread bytes across percentage, 0-1000 and pixel magnitudes.
				bbox = append(bbox, int(b)*(1+int(data[0])%16))
			}
			items = append(items, WordItem{
				No: len(items) + 1, Word: fmt.Sprintf("w%d", len(items)), Pos: "명", Meaning: "뜻",
				BBox: bbox, ImageWidth: 1600, ImageHeight: 2400,
			})
		}
		if len(items) == 0 {
			return
		}

		out := cleanWordItems(items)
		bboxInvariants(t, "fuzz", out)
		if again := cleanWordItems(out); !reflect.DeepEqual(snapshotBoxes(out), snapshotBoxes(again)) {
			t.Fatalf("not idempotent:\n  once:  %v\n  twice: %v", snapshotBoxes(out), snapshotBoxes(again))
		}
	})
}
