package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"vocat-input/internal/engine"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateImageUpload(t *testing.T) {
	mk := func(name string, size int64) *multipart.FileHeader {
		return &multipart.FileHeader{Filename: name, Size: size}
	}

	cases := []struct {
		name string
		file *multipart.FileHeader
		want string // empty means accepted
	}{
		{"small jpg", mk("page.jpg", 1024), ""},
		{"uppercase ext", mk("PAGE.JPG", 1024), ""},
		{"heic", mk("scan.heic", 1024), ""},
		{"png", mk("a.png", 1), ""},
		{"oversized", mk("big.jpg", maxImageBytes+1), "maximum"},
		{"exact limit", mk("edge.jpg", maxImageBytes), ""},
		{"executable", mk("payload.exe", 1024), "unsupported file type"},
		{"no extension", mk("README", 1024), "unsupported file type"},
		{"html disguised", mk("x.html", 1024), "unsupported file type"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateImageUpload(tc.file)
			if tc.want == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.want)
			}
		})
	}
}

// withStore swaps the package-level store for the duration of a test, since the handlers reach for
// it directly rather than taking it as a parameter.
func withStore(t *testing.T, storageDir string) {
	t.Helper()
	orig := store
	store = engine.NewRunStore(storageDir)
	t.Cleanup(func() { store = orig })
}

// newMultipartRequest builds a POST /runs multipart/form-data carrying the given files.
func newMultipartRequest(t *testing.T, files map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for name, content := range files {
		fw, err := w.CreateFormFile("images", name)
		require.NoError(t, err)
		_, err = fw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, "/runs", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// The collision fix: uploads are stored under a run-scoped name, so two runs posting the same
// client filename cannot overwrite each other's file or end up sharing one image.
func TestHandleCreateRun_NamespacesStoredFiles(t *testing.T) {
	dir := t.TempDir()
	uploadDir := filepath.Join(dir, "uploads")
	require.NoError(t, os.MkdirAll(uploadDir, 0o755))
	withStore(t, dir)

	r := gin.New()
	r.POST("/runs", handleCreateRun(uploadDir))

	// First run uploads "page.jpg".
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, newMultipartRequest(t, map[string]string{"page.jpg": "first-image"}))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var run1 engine.ConversionRun
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &run1))
	storedName1 := run1.OCRResults[0].ImageName
	assert.True(t, strings.HasPrefix(storedName1, run1.ID+"-"), "stored name %q should be namespaced under the run id", storedName1)
	assert.True(t, strings.HasSuffix(storedName1, "-page.jpg"))
	assert.Equal(t, "/uploads/"+storedName1, run1.OCRResults[0].ImagePath)
	assert.FileExists(t, filepath.Join(uploadDir, storedName1))

	// A second run with the same client filename must land in its own file, not overwrite the first.
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, newMultipartRequest(t, map[string]string{"page.jpg": "second-image"}))
	require.Equal(t, http.StatusCreated, rec2.Code, rec2.Body.String())
	var run2 engine.ConversionRun
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &run2))
	storedName2 := run2.OCRResults[0].ImageName
	assert.NotEqual(t, storedName1, storedName2, "the two runs must not share a stored file name")

	// Both files still hold their own bytes: run 1 was not clobbered by run 2.
	got1, err := os.ReadFile(filepath.Join(uploadDir, storedName1))
	require.NoError(t, err)
	assert.Equal(t, "first-image", string(got1))
	got2, err := os.ReadFile(filepath.Join(uploadDir, storedName2))
	require.NoError(t, err)
	assert.Equal(t, "second-image", string(got2))
}

func TestHandleCreateRun_RejectsNonImageAndExcessCount(t *testing.T) {
	dir := t.TempDir()
	uploadDir := filepath.Join(dir, "uploads")
	require.NoError(t, os.MkdirAll(uploadDir, 0o755))
	withStore(t, dir)

	r := gin.New()
	r.POST("/runs", handleCreateRun(uploadDir))

	// A non-image extension is refused, and nothing is written to disk.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, newMultipartRequest(t, map[string]string{"payload.exe": "x"}))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "unsupported file type")
	matches, _ := filepath.Glob(filepath.Join(uploadDir, "*"))
	assert.Empty(t, matches, "no file should be written for a rejected upload")

	// More than maxUploadImages files is refused.
	tooMany := map[string]string{}
	for i := 0; i <= maxUploadImages; i++ {
		tooMany["img"+strconv.Itoa(i)+".jpg"] = "x"
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, newMultipartRequest(t, tooMany))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "Too many images")
}
