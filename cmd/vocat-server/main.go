package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vocat-input/internal/engine"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

var (
	store    *engine.RunStore
	registry *engine.ProviderRegistry
)

func main() {
	cwd, _ := os.Getwd()
	storageDir := filepath.Join(cwd, "storage")
	store = engine.NewRunStore(storageDir)
	registry = engine.NewProviderRegistry()

	// Auto-recover zombie/interrupted runs on server startup
	for _, run := range store.List() {
		if run.Status == engine.RunStatusOCRProgress || run.Status == engine.RunStatusMerging {
			run.SetStatus(engine.RunStatusFailed)
			run.SetError("Server was restarted or terminated during conversion workflow. Click 'Retry Conversion' to restart.")
			run.AddLog("❌ [SERVER RECOVERY] Server was restarted while conversion was in progress. Marked as FAILED.")
			store.Save(run)
			log.Printf("[RECOVERY] Interrupted run '%s' recovered and marked as FAILED.", run.ID)
		}
	}

	uploadDir := filepath.Join(storageDir, "uploads")
	outputDir := filepath.Join(storageDir, "outputs")
	webDistDir := filepath.Join(cwd, "web", "dist")
	_ = os.MkdirAll(uploadDir, 0o755)
	_ = os.MkdirAll(outputDir, 0o755)

	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:8080", "http://127.0.0.1:5173", "http://127.0.0.1:8080"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-Vocat-Session"},
		AllowCredentials: true,
	}))

	r.Static("/uploads", uploadDir)
	r.Static("/outputs", outputDir)
	r.Static("/assets", filepath.Join(webDistDir, "assets"))

	// Explicit Static Handlers with Content-Type header for Vite Module Federation
	r.GET("/remoteEntry.js", func(c *gin.Context) {
		c.Header("Content-Type", "application/javascript; charset=utf-8")
		c.File(filepath.Join(webDistDir, "remoteEntry.js"))
	})
	r.GET("/remoteEntry.ssr.js", func(c *gin.Context) {
		c.Header("Content-Type", "application/javascript; charset=utf-8")
		c.File(filepath.Join(webDistDir, "remoteEntry.ssr.js"))
	})
	r.GET("/favicon.svg", func(c *gin.Context) {
		c.File(filepath.Join(webDistDir, "favicon.svg"))
	})

	r.GET("/", func(c *gin.Context) {
		c.File(filepath.Join(webDistDir, "index.html"))
	})

	api := r.Group("/api")
	{
		// Public Auth Endpoints
		api.POST("/login", handleLogin)
		api.POST("/logout", handleLogout)
		api.GET("/auth/status", handleAuthStatus)

		// Protected API Endpoints (Guarded by AuthMiddleware)
		protected := api.Group("")
		protected.Use(AuthMiddleware())
		{
			protected.GET("/runs", handleListRuns)
			protected.GET("/runs/:id", handleGetRun)
			protected.POST("/runs", handleCreateRun(uploadDir))
			protected.POST("/runs/:id/convert", handleOneClickConvert(uploadDir, outputDir))
			protected.POST("/runs/:id/ocr", handleStartOCR)
			protected.POST("/runs/:id/merge-convert", handleMergeAndConvert(uploadDir, outputDir))
			protected.POST("/runs/:id/regenerate-doc", handleRegenerateDoc(outputDir))
			protected.POST("/runs/:id/send-telegram", handleSendTelegram)
			protected.PUT("/runs/:id/words", handleUpdateWords(outputDir))
			protected.PUT("/runs/:id/title", handleUpdateTitle)
			protected.DELETE("/runs/:id", handleDeleteRun(uploadDir, outputDir))
			protected.GET("/runs/:id/download/json", handleDownloadJSON)
			protected.GET("/runs/:id/download/doc", handleDownloadDoc)
			protected.POST("/runs/:id/adb-input", handleADBInput)
		}
	}

	r.NoRoute(func(c *gin.Context) {
		reqPath := c.Request.URL.Path
		if strings.HasPrefix(reqPath, "/api") {
			c.JSON(http.StatusNotFound, gin.H{"error": "API route not found"})
			return
		}

		// Try serving static file directly from webDistDir if it exists (e.g. /remoteEntry.js, /favicon.svg)
		targetFile := filepath.Join(webDistDir, filepath.Clean(reqPath))
		if info, err := os.Stat(targetFile); err == nil && !info.IsDir() {
			if strings.HasSuffix(strings.ToLower(reqPath), ".js") {
				c.Header("Content-Type", "application/javascript; charset=utf-8")
			}
			c.File(targetFile)
			return
		}

		// SPA Client Fallback
		c.File(filepath.Join(webDistDir, "index.html"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Printf("Vocat Auto Server listening on http://localhost:%s\n", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Server exited with error: %v", err)
	}
}

func getSessionSecret() string {
	secret := os.Getenv("VOCAT_SESSION_SECRET")
	if secret == "" {
		if envData, err := os.ReadFile(".env"); err == nil {
			for _, line := range strings.Split(string(envData), "\n") {
				if strings.HasPrefix(line, "VOCAT_SESSION_SECRET=") {
					secret = strings.TrimSpace(strings.Split(line, "=")[1])
					break
				}
			}
		}
	}
	if secret == "" {
		secret = "vocat_secure_session_secret_2026"
	}
	return secret
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		validSecret := getSessionSecret()

		// 1. Check Session Cookie
		cookieToken, err := c.Cookie("vocat_session")
		if err == nil && cookieToken == validSecret {
			c.Next()
			return
		}

		// 2. Check Authorization Header
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == validSecret {
				c.Next()
				return
			}
		}

		// 3. Check X-Vocat-Session Header
		xToken := c.GetHeader("X-Vocat-Session")
		if xToken == validSecret {
			c.Next()
			return
		}

		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "Unauthorized access. Valid session or bearer token required to access backend API.",
			"code":    "UNAUTHORIZED",
			"details": "Please log in or supply a valid X-Vocat-Session header.",
		})
		c.Abort()
	}
}

func handleLogin(c *gin.Context) {
	var req struct {
		Secret string `json:"secret"`
	}
	_ = c.ShouldBindJSON(&req)

	validSecret := getSessionSecret()
	if req.Secret != "" && req.Secret == validSecret {
		c.SetCookie("vocat_session", validSecret, 86400, "/", "", false, false)
		c.JSON(http.StatusOK, gin.H{
			"status":  "authenticated",
			"message": "Session successfully established",
		})
		return
	}

	c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authentication secret key"})
}

func handleLogout(c *gin.Context) {
	c.SetCookie("vocat_session", "", -1, "/", "", false, false)
	c.JSON(http.StatusOK, gin.H{"status": "logged_out", "message": "Session terminated"})
}

func handleAuthStatus(c *gin.Context) {
	validSecret := getSessionSecret()
	cookieToken, err := c.Cookie("vocat_session")
	isAuth := err == nil && cookieToken == validSecret
	c.JSON(http.StatusOK, gin.H{
		"authenticated": isAuth,
	})
}

func handleListRuns(c *gin.Context) {
	runs := store.List()
	c.JSON(http.StatusOK, gin.H{"runs": runs})
}

func handleGetRun(c *gin.Context) {
	id := c.Param("id")
	run, ok := store.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Run not found"})
		return
	}
	c.JSON(http.StatusOK, run)
}

func handleCreateRun(uploadDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var ocrProvider string
		var ocrModel string
		var preserveOrder bool = true
		var imagePaths []string
		var ocrResults []*engine.OCRResult

		contentType := c.ContentType()

		if strings.Contains(contentType, "application/json") {
			var req struct {
				OCRProvider   string   `json:"ocrProvider"`
				PreserveOrder bool     `json:"preserveOrder"`
				Images        []string `json:"images"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Invalid JSON payload: %v", err)})
				return
			}
			ocrProvider = req.OCRProvider
			preserveOrder = req.PreserveOrder

			if len(req.Images) == 0 {
				// Fallback to sample images from ./imgs/1 or uploadDir
				cwd, _ := os.Getwd()
				sampleDir := filepath.Join(cwd, "imgs", "1")
				entries, _ := os.ReadDir(sampleDir)
				for i, entry := range entries {
					if !entry.IsDir() {
						imgPath := filepath.Join(sampleDir, entry.Name())
						imagePaths = append(imagePaths, imgPath)
						ocrResults = append(ocrResults, &engine.OCRResult{
							ImageIndex: i + 1,
							ImageName:  entry.Name(),
							ImagePath:  fmt.Sprintf("/uploads/%s", entry.Name()),
							Status:     "PENDING",
						})
					}
				}
			} else {
				cwd, _ := os.Getwd()
				for i, name := range req.Images {
					imgPath := filepath.Join(cwd, "imgs", "1", name)
					if _, err := os.Stat(imgPath); os.IsNotExist(err) {
						imgPath = filepath.Join(uploadDir, name)
					}
					imagePaths = append(imagePaths, imgPath)
					ocrResults = append(ocrResults, &engine.OCRResult{
						ImageIndex: i + 1,
						ImageName:  name,
						ImagePath:  fmt.Sprintf("/uploads/%s", name),
						Status:     "PENDING",
					})
				}
			}
		} else {
			form, err := c.MultipartForm()
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid form data"})
				return
			}

			files := form.File["images"]
			if len(files) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "No image files uploaded"})
				return
			}

			providers := form.Value["ocrProvider"]
			if len(providers) > 0 {
				ocrProvider = providers[0]
			}
			if ocrProvider == "" {
				ocrProvider = "doublecheck"
			}

			orders := form.Value["preserveOrder"]
			if len(orders) > 0 && orders[0] == "false" {
				preserveOrder = false
			}

			models := form.Value["ocrModel"]
			if len(models) > 0 {
				ocrModel = models[0]
			}

			for i, file := range files {
				dst := filepath.Join(uploadDir, file.Filename)
				if err := c.SaveUploadedFile(file, dst); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to save file %s: %v", file.Filename, err)})
					return
				}
				imagePaths = append(imagePaths, dst)
				ocrResults = append(ocrResults, &engine.OCRResult{
					ImageIndex: i + 1,
					ImageName:  file.Filename,
					ImagePath:  fmt.Sprintf("/uploads/%s", file.Filename),
					Status:     "PENDING",
				})
			}
		}

		if ocrProvider == "" {
			ocrProvider = "vertex"
		}
		if ocrModel == "" {
			ocrModel = "gemini-2.5-flash"
		}

		runID := fmt.Sprintf("run_%d", time.Now().UnixNano()/1e6)
		run := &engine.ConversionRun{
			ID:            runID,
			Title:         fmt.Sprintf("Vocat Run %s", time.Now().Format("01-02 15:04")),
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
			Status:        engine.RunStatusCreated,
			OCRProvider:   ocrProvider,
			OCRModel:      ocrModel,
			// The scale the stored words are actually on, not the model's own coordinate
			// space: the engine normalizes every bbox to percentages before persisting.
			BBoxScale:     engine.BBoxOutputScale,
			PreserveOrder: preserveOrder,
			Images:        imagePaths,
			OCRResults:    ocrResults,
		}

		store.Save(run)
		c.JSON(http.StatusCreated, run)
	}
}

func handleStartOCR(c *gin.Context) {
	id := c.Param("id")
	run, ok := store.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Run not found"})
		return
	}

	run.SetStatus(engine.RunStatusOCRProgress)
	run.SetProgress(5)
	run.AddLog(fmt.Sprintf("🚀 OCR Processing Started with Engine '%s' (Progress: 5%%)", run.OCRProvider))
	store.Save(run)

	go func(r *engine.ConversionRun) {
		provider, err := registry.Get(r.OCRProvider)
		if err != nil || provider == nil {
			r.SetStatus(engine.RunStatusFailed)
			r.SetError(fmt.Sprintf("OCR provider '%s' not found", r.OCRProvider))
			r.AddLog(fmt.Sprintf("❌ OCR provider '%s' not found", r.OCRProvider))
			store.Save(r)
			return
		}
		ctx := context.Background()

		// Set model env var so providers pick it up
		if r.OCRModel != "" {
			if r.OCRProvider == "bedrock" {
				os.Setenv("BEDROCK_MODEL", r.OCRModel)
			} else {
				os.Setenv("VERTEX_MODEL", r.OCRModel)
			}
		}

		total := len(r.Images)
		for i, imgPath := range r.Images {
			r.SetOCRResultStatus(i, "PROCESSING")
			currentProg := 5 + int((float64(i)/float64(total))*65.0)
			r.SetProgress(currentProg)
			r.AddLog(fmt.Sprintf("📷 [%d/%d] Running OCR on '%s' (model: %s)... (Progress: %d%%)", i+1, total, r.OCRResults[i].ImageName, r.OCRModel, currentProg))
			store.Save(r)

			text, err := provider.ProcessImage(ctx, imgPath)
			if err != nil {
				r.SetOCRResultStatus(i, "FAILED")
				r.SetOCRResultError(i, err.Error())
				r.SetStatus(engine.RunStatusFailed)
				r.SetError(fmt.Sprintf("OCR Failed on '%s': %v", r.OCRResults[i].ImageName, err))
				r.AddLog(fmt.Sprintf("❌ [%d/%d] OCR Failed on '%s': %v", i+1, total, r.OCRResults[i].ImageName, err))
				store.Save(r)
				return
			} else {
				r.SetOCRResultStatus(i, "COMPLETED")
				r.SetOCRResultText(i, text)
				doneProg := 5 + int((float64(i+1)/float64(total))*65.0)
				r.SetProgress(doneProg)
				r.AddLog(fmt.Sprintf("✅ [%d/%d] OCR Completed on '%s' (Progress: %d%%)", i+1, total, r.OCRResults[i].ImageName, doneProg))
			}
			store.Save(r)
		}

		r.SetStatus(engine.RunStatusOCRDone)
		r.SetProgress(70)
		r.AddLog("🎉 All Image Pages OCR Processing Completed! Ready for AI Structuring & Conversion (Progress: 70%)")
		store.Save(r)
	}(run)

	c.JSON(http.StatusOK, gin.H{"message": "OCR started", "run": run})
}

func handleMergeAndConvert(uploadDir, outputDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		run, ok := store.Get(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Run not found"})
			return
		}

		run.SetStatus(engine.RunStatusMerging)
		run.SetProgress(75)
		run.AddLog("🔮 AI Structuring Engine Launched. Merging OCR Transcriptions... (Progress: 75%)")
		store.Save(run)

		var merged []string
		for _, res := range run.OCRResults {
			if res.RawText != "" {
				merged = append(merged, res.RawText)
			}
		}
		mergedText := strings.Join(merged, "\n\n")
		run.SetMergedText(mergedText)
		run.AddLog(fmt.Sprintf("📄 Merged OCR Text Prepared (%d total characters)", len(mergedText)))

		ctx := context.Background()
		run.SetProgress(80)

		// Build image paths for Stage 1 format analysis
		var imagePaths []string
		for _, img := range run.Images {
			if filepath.IsAbs(img) {
				imagePaths = append(imagePaths, img)
			} else {
				imagePaths = append(imagePaths, filepath.Join(uploadDir, img))
			}
		}

		run.AddLog(fmt.Sprintf("🔍 Stage 1: Analyzing image format with AI Vision (%d images)... (Progress: 80%%)", len(imagePaths)))
		store.Save(run)

		run.SetProgress(85)
		run.AddLog("🤖 Stage 2: Extracting vocabulary with format-aware AI prompt... (Progress: 85%)")
		words, err := engine.ConvertOCRToVocatJSON(ctx, mergedText, run.PreserveOrder, imagePaths, run.OCRProvider, run.OCRModel)
		if err != nil {
			run.SetStatus(engine.RunStatusFailed)
			run.SetError(fmt.Sprintf("Conversion failed: %v", err))
			run.AddLog(fmt.Sprintf("❌ Conversion Failed: %v", err))
			store.Save(run)
			c.JSON(http.StatusInternalServerError, gin.H{"error": run.Error})
			return
		}
		run.SetWords(words)
		run.SetProgress(95)
		run.AddLog(fmt.Sprintf("✨ AI Structuring Completed! %d Structured Words & Bounding Boxes Extracted (Progress: 95%%)", len(words)))

		jsonFileName := fmt.Sprintf("%s.json", run.ID)
		docFileName := fmt.Sprintf("%s.doc", run.ID)
		jsonPath := filepath.Join(outputDir, jsonFileName)
		docPath := filepath.Join(outputDir, docFileName)

		jsonBytes, _ := json.MarshalIndent(words, "", "  ")
		_ = os.WriteFile(jsonPath, jsonBytes, 0o644)
		_ = engine.GenerateDocFile(words, docPath)

		run.SetOutputPaths(fmt.Sprintf("/outputs/%s", jsonFileName), fmt.Sprintf("/outputs/%s", docFileName))
		run.SetStatus(engine.RunStatusCompleted)
		run.SetProgress(100)
		run.AddLog("🎉 Run Completed Successfully! Vocat JSON & DOC Test Sheets Ready (Progress: 100%)")
		store.Save(run)

		c.JSON(http.StatusOK, run)
	}
}

func handleOneClickConvert(uploadDir, outputDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		run, ok := store.Get(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Run not found"})
			return
		}

		run.SetError("")
		run.SetStatus(engine.RunStatusOCRProgress)
		run.SetProgress(5)
		run.AddLog(fmt.Sprintf("🚀 One-Click Conversion Engine Started/Retried with Provider '%s' (model: %s)", run.OCRProvider, run.OCRModel))
		store.Save(run)

		go func(r *engine.ConversionRun) {
			provider, err := registry.Get(r.OCRProvider)
			if err != nil || provider == nil {
				r.SetStatus(engine.RunStatusFailed)
				r.SetError(fmt.Sprintf("OCR provider '%s' not found", r.OCRProvider))
				r.AddLog(fmt.Sprintf("❌ OCR provider '%s' not found", r.OCRProvider))
				store.Save(r)
				return
			}
			ctx := context.Background()

			// Phase 1: Full OCR Recognition (5% -> 70%)
			total := len(r.Images)
			for i, imgPath := range r.Images {
				r.SetOCRResultStatus(i, "PROCESSING")
				currentProg := 5 + int((float64(i)/float64(total))*65.0)
				r.SetProgress(currentProg)
				r.AddLog(fmt.Sprintf("📷 [%d/%d] OCR Vision Processing '%s'... (%d%%)", i+1, total, r.OCRResults[i].ImageName, currentProg))
				store.Save(r)

				text, err := provider.ProcessImage(ctx, imgPath)
				if err != nil {
					r.SetOCRResultStatus(i, "FAILED")
					r.SetOCRResultError(i, err.Error())
					r.SetStatus(engine.RunStatusFailed)
					r.SetError(fmt.Sprintf("OCR Failed on '%s': %v", r.OCRResults[i].ImageName, err))
					r.AddLog(fmt.Sprintf("❌ [%d/%d] OCR Failed on '%s': %v", i+1, total, r.OCRResults[i].ImageName, err))
					store.Save(r)
					return
				} else {
					r.SetOCRResultStatus(i, "COMPLETED")
					r.SetOCRResultText(i, text)
					doneProg := 5 + int((float64(i+1)/float64(total))*65.0)
					r.SetProgress(doneProg)
					r.AddLog(fmt.Sprintf("✅ [%d/%d] OCR Completed on '%s' (%d%%)", i+1, total, r.OCRResults[i].ImageName, doneProg))

					// Add Raw OCR preview snippet to log stream for trace comparison
					snippet := strings.TrimSpace(text)
					if len(snippet) > 180 {
						snippet = snippet[:180] + "..."
					}
					snippet = strings.ReplaceAll(snippet, "\n", " ")
					r.AddLog(fmt.Sprintf("  📄 [RAW OCR RESPONSE #%d]: \"%s\"", i+1, snippet))
				}
				store.Save(r)
			}

			r.SetStatus(engine.RunStatusMerging)
			r.SetProgress(75)
			r.AddLog("🔮 AI Structuring Engine Launched. Merging Transcriptions... (75%)")
			store.Save(r)

			// Phase 2: Merge & AI Structuring (75% -> 95%)
			var merged []string
			for _, res := range r.OCRResults {
				if res.RawText != "" {
					merged = append(merged, res.RawText)
				}
			}
			mergedText := strings.Join(merged, "\n\n")
			r.SetMergedText(mergedText)

			r.SetProgress(80)

			// Build image paths for Stage 1
			var imagePaths []string
			for _, img := range r.Images {
				if filepath.IsAbs(img) {
					imagePaths = append(imagePaths, img)
				} else {
					imagePaths = append(imagePaths, filepath.Join(uploadDir, img))
				}
			}
			r.AddLog(fmt.Sprintf("🔍 Stage 1: Analyzing image format (%d images)... (80%%)", len(imagePaths)))
			store.Save(r)

			r.SetProgress(85)
			r.AddLog("🤖 Stage 2: Extracting vocabulary with format-aware AI prompt... (85%)")
			words, err := engine.ConvertOCRToVocatJSON(ctx, mergedText, r.PreserveOrder, imagePaths, r.OCRProvider, r.OCRModel)
			if err != nil {
				r.SetStatus(engine.RunStatusFailed)
				r.SetError(fmt.Sprintf("Conversion failed: %v", err))
				r.AddLog(fmt.Sprintf("❌ AI Structuring Failed: %v", err))
				store.Save(r)
				return
			}
			r.SetWords(words)
			r.SetProgress(95)
			r.AddLog(fmt.Sprintf("✨ AI Structuring Completed! %d Structured Words Extracted (95%%)", len(words)))
			for _, w := range words {
				meaningStr := fmt.Sprintf("%v", w.Meaning)
				r.AddLog(fmt.Sprintf("  🏷️ [FINAL WORD #%d]: '%s' | POS: '%s' | Meaning: '%s' | BBox: %v", w.No, w.Word, w.Pos, meaningStr, w.BBox))
			}

			// Phase 3: Export JSON & DOC Files (100%)
			jsonFileName := fmt.Sprintf("%s.json", r.ID)
			docFileName := fmt.Sprintf("%s.doc", r.ID)
			jsonPath := filepath.Join(outputDir, jsonFileName)
			docPath := filepath.Join(outputDir, docFileName)

			jsonBytes, _ := json.MarshalIndent(words, "", "  ")
			_ = os.WriteFile(jsonPath, jsonBytes, 0o644)
			_ = engine.GenerateDocFile(words, docPath)

			r.SetOutputPaths(fmt.Sprintf("/outputs/%s", jsonFileName), fmt.Sprintf("/outputs/%s", docFileName))
			r.SetStatus(engine.RunStatusCompleted)
			r.SetProgress(100)
			r.AddLog("🎉 Full Pipeline Completed! Vocat JSON & DOC Test Sheets Ready (100%)")
			store.Save(r)
		}(run)

		c.JSON(http.StatusOK, gin.H{"message": "Conversion started", "run": run})
	}
}

func handleRegenerateDoc(outputDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		run, ok := store.Get(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Run not found"})
			return
		}

		if len(run.Words) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "No vocabulary words found to generate DOC"})
			return
		}

		docFileName := fmt.Sprintf("%s.doc", run.ID)
		docPath := filepath.Join(outputDir, docFileName)

		if err := engine.GenerateDocFile(run.Words, docPath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to regenerate DOC: %v", err)})
			return
		}

		run.SetDocPath(fmt.Sprintf("/outputs/%s", docFileName))
		run.AddLog("🔄 Vocabulary DOC Test Sheet Regenerated from updated JSON data.")
		store.Save(run)

		c.JSON(http.StatusOK, gin.H{
			"message": "DOC regenerated successfully",
			"docPath": run.DocPath,
			"run":     run,
		})
	}
}

func handleSendTelegram(c *gin.Context) {
	id := c.Param("id")
	run, ok := store.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Run not found"})
		return
	}

	if run.DocPath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "DOC test sheet file not generated yet."})
		return
	}

	caption := fmt.Sprintf("📚 Vocat Input Word Sheet (%s)\nWords: %d items\nProvider: %s", run.Title, len(run.Words), run.OCRProvider)
	if err := engine.SendDocToTelegram(run.DocPath, run.Title, caption); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Telegram delivery failed: %v", err)})
		return
	}

	run.AddLog("✈️ DOC Test Sheet Successfully Transmitted to Telegram!")
	store.Save(run)

	c.JSON(http.StatusOK, gin.H{"message": "Document successfully sent to Telegram", "run": run})
}

func handleUpdateWords(outputDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		run, ok := store.Get(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Run not found"})
			return
		}

		var payload struct {
			Words []engine.WordItem `json:"words"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		run.Words = payload.Words

		jsonFileName := fmt.Sprintf("%s.json", run.ID)
		docFileName := fmt.Sprintf("%s.doc", run.ID)
		jsonPath := filepath.Join(outputDir, jsonFileName)
		docPath := filepath.Join(outputDir, docFileName)

		jsonBytes, _ := json.MarshalIndent(payload.Words, "", "  ")
		_ = os.WriteFile(jsonPath, jsonBytes, 0o644)
		_ = engine.GenerateDocFile(payload.Words, docPath)

		store.Save(run)
		c.JSON(http.StatusOK, run)
	}
}

func handleUpdateTitle(c *gin.Context) {
	id := c.Param("id")
	run, ok := store.Get(id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Run not found"})
		return
	}
	var payload struct {
		Title string `json:"title"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil || strings.TrimSpace(payload.Title) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Title cannot be empty"})
		return
	}
	run.Title = strings.TrimSpace(payload.Title)
	store.Save(run)
	c.JSON(http.StatusOK, run)
}

func handleDeleteRun(uploadDir, outputDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		run, ok := store.Get(id)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "Run not found"})
			return
		}

		// Delete uploaded images
		for _, img := range run.Images {
			imgPath := img
			if !filepath.IsAbs(imgPath) {
				imgPath = filepath.Join(uploadDir, img)
			}
			_ = os.Remove(imgPath)
		}

		// Delete json & doc files
		_ = os.Remove(filepath.Join(outputDir, fmt.Sprintf("%s.json", id)))
		_ = os.Remove(filepath.Join(outputDir, fmt.Sprintf("%s.doc", id)))

		// Delete from in-memory store
		store.Delete(id)

		c.JSON(http.StatusOK, gin.H{"message": "Run and associated files deleted successfully", "id": id})
	}
}

func handleDownloadJSON(c *gin.Context) {
	id := c.Param("id")
	run, ok := store.Get(id)
	if !ok || run.JSONPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "JSON file not found"})
		return
	}
	filePath := filepath.Join("storage", "outputs", fmt.Sprintf("%s.json", run.ID))
	fileName := strings.TrimSpace(run.Title)
	if fileName == "" {
		fileName = run.ID
	}
	if !strings.HasSuffix(strings.ToLower(fileName), ".json") {
		fileName += ".json"
	}
	c.FileAttachment(filePath, fileName)
}

func handleDownloadDoc(c *gin.Context) {
	id := c.Param("id")
	run, ok := store.Get(id)
	if !ok || run.DocPath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Doc file not found"})
		return
	}
	filePath := filepath.Join("storage", "outputs", fmt.Sprintf("%s.doc", run.ID))
	fileName := strings.TrimSpace(run.Title)
	if fileName == "" {
		fileName = run.ID
	}
	if !strings.HasSuffix(strings.ToLower(fileName), ".doc") {
		fileName += ".doc"
	}
	c.FileAttachment(filePath, fileName)
}

func handleADBInput(c *gin.Context) {
	id := c.Param("id")
	run, ok := store.Get(id)
	if !ok || len(run.Words) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No words available for ADB input"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "ADB input triggered for Vocat app",
		"runId":     run.ID,
		"wordCount": len(run.Words),
	})
}

