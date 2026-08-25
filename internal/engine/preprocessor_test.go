package engine

import (
	"context"
	"image"
	"image/color"
	"path/filepath"
	"testing"

	"github.com/disintegration/imaging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRotationResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"exact json 0", `{"rotation": 0}`, 0},
		{"exact json 90", `{"rotation": 90}`, 90},
		{"exact json 180", `{"rotation": 180}`, 180},
		{"exact json 270", `{"rotation": 270}`, 270},
		{"markdown wrapped", "```json\n{\"rotation\": 90}\n```", 90},
		{"with explanation", `The text is sideways. {"rotation": 270} is needed.`, 270},
		{"fallback search 180", `Rotate 180 degrees`, 180},
		{"fallback default 0", `Already upright`, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRotationResponse(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestPreprocessImage_ResizeAndEnhance(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test_page.jpg")

	// Create a large mock image (3000x2000)
	src := image.NewRGBA(image.Rect(0, 0, 3000, 2000))
	for y := 0; y < 2000; y++ {
		for x := 0; x < 3000; x++ {
			src.Set(x, y, color.RGBA{R: 240, G: 240, B: 240, A: 255})
		}
	}
	require.NoError(t, imaging.Save(src, imgPath))

	// Run preprocessing without autoRotate (offline test)
	res, err := PreprocessImage(context.Background(), imgPath, false)
	require.NoError(t, err)

	assert.Equal(t, 3000, res.OriginalWidth)
	assert.Equal(t, 2000, res.OriginalHeight)
	assert.Equal(t, 2048, res.NewWidth)
	assert.Equal(t, 1365, res.NewHeight)
	assert.Equal(t, 0, res.RotationAngle)
	assert.True(t, res.Enhanced)

	// Verify image on disk was resized
	saved, err := imaging.Open(imgPath)
	require.NoError(t, err)
	assert.Equal(t, 2048, saved.Bounds().Dx())
	assert.Equal(t, 1365, saved.Bounds().Dy())
}
