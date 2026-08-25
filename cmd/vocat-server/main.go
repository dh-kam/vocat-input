package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	_ = os.MkdirAll(storageDir, 0o755)
	store = engine.NewRunStore(storageDir)
	registry = engine.NewProviderRegistry()

	var err error
	if sessionSecret, err = resolveSessionSecret(storageDir); err != nil {
		log.Fatalf("Cannot establish a session secret: %v", err)
	}

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
	_ = r.SetTrustedProxies(nil)
	r.Use(cors.New(cors.Config{
		AllowOriginFunc:  isAllowedOrigin,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-Vocat-Session"},
		AllowCredentials: true,
	}))

	r.Static("/assets", filepath.Join(webDistDir, "assets"))

	// /uploads and /outputs hold user material — source photographs and generated word sheets — so
	// they sit behind the same session as the API rather than on the open root engine. Same-origin
	// requests carry the cookie automatically, which covers the SPA's <img>, canvas and fetch reads.
	// They stay on the root engine (not under /api) to preserve the URL shape the frontend builds.
	guarded := r.Group("")
	guarded.Use(AuthMiddleware())
	guarded.Static("/uploads", uploadDir)
	guarded.Static("/outputs", outputDir)

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

	r.GET("/", serveSPA(webDistDir))

	api := r.Group("/api")
	{
		// Public Auth Endpoints
		// /login is rate-limited per client IP. In authRequired mode the SPA posts the
		// administrator password here to receive a derived session cookie.
		api.POST("/login", rateLimitByIP(newIPLimiter(10, time.Minute)), handleLogin)
		api.POST("/logout", handleLogout)
		api.GET("/auth/status", handleAuthStatus)
		api.GET("/models", handleGetModels)

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
			protected.POST("/runs/:id/send-telegram", handleSendTelegram(outputDir))
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

		// SPA Client Fallback — only issue auto-session when auth is not strictly required
		if !authRequired {
			issueSessionCookie(c)
		}
		c.File(filepath.Join(webDistDir, "index.html"))
	})

	port := engine.LookupConfig("PORT")
	if port == "" {
		port = "8080"
	}
	bindHost := resolveBindHost()
	if err := enforceBindAuth(bindHost); err != nil {
		log.Fatalf("%v", err)
	}
	bindAddr := fmt.Sprintf("%s:%s", bindHost, port)

	srv := &http.Server{
		Addr:    bindAddr,
		Handler: r,
	}

	go func() {
		fmt.Printf("Vocat Auto Server listening on http://%s (authRequired: %v)\n", bindAddr, authRequired)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server exited with error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("[SHUTDOWN] Shutting down Vocat Auto Server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("[SHUTDOWN ERROR] Server forced to shutdown: %v", err)
	}

	// Cleanly mark any in-flight runs as FAILED so state is persisted on disk
	for _, run := range store.List() {
		if run.Status == engine.RunStatusOCRProgress || run.Status == engine.RunStatusMerging {
			run.SetStatus(engine.RunStatusFailed)
			run.SetError("Server was gracefully stopped during conversion workflow. Click 'Retry Conversion' to restart.")
			run.AddLog("⚠️ [SERVER SHUTDOWN] Server stopped. Marked as FAILED.")
			store.Save(run)
		}
	}
	log.Println("[SHUTDOWN] Vocat Auto Server cleanly stopped.")
}

// sessionSecret authenticates non-browser API clients through the Authorization or
// X-Vocat-Session header.
var (
	sessionSecret string
	adminPassword string
	authRequired  bool
)

const sessionCookieMaxAge = 86400

// resolveBindHost picks the listen address. VOCAT_BIND_HOST wins when set; otherwise
// VOCAT_LOCAL_ONLY=true forces loopback and the default remains 0.0.0.0.
func resolveBindHost() string {
	if h := engine.LookupConfig("VOCAT_BIND_HOST"); h != "" {
		return h
	}
	if strings.ToLower(engine.LookupConfig("VOCAT_LOCAL_ONLY")) == "true" {
		return "127.0.0.1"
	}
	return "0.0.0.0"
}

func isLoopbackBindHost(host string) bool {
	h := strings.TrimSpace(strings.ToLower(host))
	if h == "127.0.0.1" || h == "::1" || h == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// enforceBindAuth configures authentication requirements based on environment settings.
// If an administrator password is configured or VOCAT_REQUIRE_AUTH=true, authRequired is enabled.
// Otherwise, the server operates in convenience mode (automatic session issuing on GET /).
func enforceBindAuth(bindHost string) error {
	if adminPassword != "" || strings.ToLower(engine.LookupConfig("VOCAT_REQUIRE_AUTH")) == "true" {
		if adminPassword == "" {
			return fmt.Errorf("VOCAT_REQUIRE_AUTH is set but VOCAT_ADMIN_PASSWORD (or ADMIN_PASSWORD) is empty")
		}
		authRequired = true
		log.Printf("[AUTH] Authentication required mode active on %s", bindHost)
	} else {
		authRequired = false
		log.Printf("[AUTH] Convenience mode active on %s (automatic session issuing)", bindHost)
	}
	return nil
}

// isAllowedOrigin strictly validates that the request origin belongs to localhost,
// loopback, private RFC1918 subnets, or explicit VOCAT_ALLOWED_ORIGINS entries.
func isAllowedOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	hostname := u.Hostname()
	if hostname == "localhost" || hostname == "127.0.0.1" {
		return true
	}
	ip := net.ParseIP(hostname)
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		return true
	}
	if allowed := engine.LookupConfig("VOCAT_ALLOWED_ORIGINS"); allowed != "" {
		for _, item := range strings.Split(allowed, ",") {
			trimmed := strings.TrimSpace(item)
			if trimmed != "" && (trimmed == origin || trimmed == u.Host || trimmed == hostname) {
				return true
			}
		}
	}
	return false
}

// resolveSessionSecret prefers an explicit VOCAT_SESSION_SECRET. Failing that it generates one
// and keeps it in storage/session_secret, so no configuration is required, the value survives
// restarts (a fresh one each boot would invalidate every open tab's cookie), and an API client
// can read it from that file.
func resolveSessionSecret(storageDir string) (string, error) {
	adminPassword = engine.LookupConfig("VOCAT_ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = engine.LookupConfig("ADMIN_PASSWORD")
	}
	authRequired = adminPassword != "" || strings.ToLower(engine.LookupConfig("VOCAT_REQUIRE_AUTH")) == "true"
	if authRequired && adminPassword == "" {
		return "", fmt.Errorf("VOCAT_REQUIRE_AUTH is set but VOCAT_ADMIN_PASSWORD (or ADMIN_PASSWORD) is empty")
	}

	if s := engine.LookupConfig("VOCAT_SESSION_SECRET"); s != "" {
		return s, nil
	}

	path := filepath.Join(storageDir, "session_secret")
	if existing, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(existing)); s != "" {
			return s, nil
		}
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating a session secret: %w", err)
	}
	secret := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	log.Printf("[AUTH] VOCAT_SESSION_SECRET is not set; generated one and stored it in %s (authRequired=%v)", path, authRequired)
	return secret, nil
}

// mintSessionToken returns an HMAC-signed session token bound to sessionSecret.
// The cookie never carries the secret itself, so stealing it does not reveal
// VOCAT_SESSION_SECRET and cannot be reused after expiry.
func mintSessionToken() (string, error) {
	if sessionSecret == "" {
		return "", fmt.Errorf("session secret is not configured")
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	exp := time.Now().Add(time.Duration(sessionCookieMaxAge) * time.Second).Unix()
	payload := fmt.Sprintf("%d.%s", exp, hex.EncodeToString(nonce))
	mac := hmac.New(sha256.New, []byte(sessionSecret))
	mac.Write([]byte(payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

func validSessionToken(tok string) bool {
	if tok == "" || sessionSecret == "" {
		return false
	}
	i := strings.LastIndex(tok, ".")
	if i <= 0 || i == len(tok)-1 {
		return false
	}
	payload, macHex := tok[:i], tok[i+1:]
	mac, err := hex.DecodeString(macHex)
	if err != nil {
		return false
	}
	h := hmac.New(sha256.New, []byte(sessionSecret))
	h.Write([]byte(payload))
	if !hmac.Equal(h.Sum(nil), mac) {
		return false
	}
	dot := strings.IndexByte(payload, '.')
	if dot <= 0 {
		return false
	}
	exp, err := strconv.ParseInt(payload[:dot], 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < exp
}

func passwordMatches(given string) bool {
	if adminPassword == "" || given == "" {
		return false
	}
	want := sha256.Sum256([]byte(adminPassword))
	got := sha256.Sum256([]byte(given))
	return hmac.Equal(want[:], got[:])
}

// issueSessionCookie grants the browser a derived session token. httpOnly keeps it
// away from scripts on the origin.
func issueSessionCookie(c *gin.Context) {
	tok, err := mintSessionToken()
	if err != nil {
		log.Printf("[AUTH] failed to mint session token: %v", err)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "vocat_session",
		Value:    tok,
		Path:     "/",
		MaxAge:   sessionCookieMaxAge,
		HttpOnly: true,
		Secure:   c.Request.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

// serveSPA returns index.html and, in non-auth-required mode, the session the app needs to call the API.
func serveSPA(webDistDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authRequired {
			issueSessionCookie(c)
		}
		c.File(filepath.Join(webDistDir, "index.html"))
	}
}

// tokenMatches compares in constant time so a valid secret cannot be recovered a byte at a time.
func tokenMatches(given string) bool {
	return given != "" && hmac.Equal([]byte(given), []byte(sessionSecret))
}

func headerCredentialOK(given string) bool {
	return validSessionToken(given) || tokenMatches(given)
}

// isAuthenticated accepts a derived session cookie, or for non-browser clients a
// derived token or the raw session secret in Authorization / X-Vocat-Session.
func isAuthenticated(c *gin.Context) bool {
	if cookieToken, err := c.Cookie("vocat_session"); err == nil && validSessionToken(cookieToken) {
		return true
	}
	if bearer, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer "); ok && headerCredentialOK(bearer) {
		return true
	}
	return headerCredentialOK(c.GetHeader("X-Vocat-Session"))
}

func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isAuthenticated(c) {
			c.Next()
			return
		}

		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "Unauthorized access. Valid session or bearer token required to access backend API.",
			"code":    "UNAUTHORIZED",
			"details": "Authenticate via /api/login or supply a valid session cookie / X-Vocat-Session header.",
		})
		c.Abort()
	}
}

// handleLogin issues a derived session cookie only after the administrator password matches.
// The session secret is never accepted here — it remains a non-browser header credential.
func handleLogin(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
		Secret   string `json:"secret"`
	}
	_ = c.ShouldBindJSON(&req)

	if !passwordMatches(req.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authentication credentials"})
		return
	}

	tok, err := mintSessionToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to establish session"})
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "vocat_session",
		Value:    tok,
		Path:     "/",
		MaxAge:   sessionCookieMaxAge,
		HttpOnly: true,
		Secure:   c.Request.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	c.JSON(http.StatusOK, gin.H{
		"status":  "authenticated",
		"message": "Session successfully established",
	})
}

func handleLogout(c *gin.Context) {
	c.SetCookie("vocat_session", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"status": "logged_out", "message": "Session terminated"})
}

func handleAuthStatus(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"authenticated": isAuthenticated(c),
		"authRequired":  authRequired,
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

// resolveInDir joins a caller-supplied file name onto dir and confirms the result stays inside
// it.
//
// Names arriving from a request are never trusted: the JSON branch of handleCreateRun used to
// join them straight into a filesystem path, so {"images":["../../.env"]} became an absolute
// path outside the storage tree that the OCR providers then read and handed to a vision API,
// and that handleDeleteRun later unlinked. Every image this server stores is a flat file name,
// so anything carrying a path separator is rejected outright rather than quietly rewritten -
// a traversal attempt should fail loudly, not succeed against a different file.
func resolveInDir(dir, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("empty file name")
	}
	if name != filepath.Base(name) || name == "." || name == ".." {
		return "", fmt.Errorf("invalid file name %q", name)
	}

	full := filepath.Join(dir, name)
	if !containedIn(dir, full) {
		return "", fmt.Errorf("file name %q escapes the storage directory", name)
	}
	return full, nil
}

// containedIn reports whether path lives inside dir. Both are resolved to absolute paths first
// so a stored path from an older run is compared on the same footing.
func containedIn(dir, path string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Limits and the image extension allowlist for multipart uploads. These bound the disk and memory
// a single create-run request can claim and keep non-image files out of a directory the OCR
// providers will hand to vision APIs and the server will serve back over /uploads.
const (
	maxUploadImages = 30
	maxImageBytes   = 32 << 20  // 32 MiB per image
	maxUploadBytes  = 256 << 20 // 256 MiB total per request
)

var allowedImageExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true, ".bmp": true, ".heic": true,
}

// validateImageUpload rejects oversized or non-image uploads before they reach disk. The extension
// is the gate rather than MIME sniffing because r.Static types files by extension anyway, so a
// .jpg-named HTML payload is served as image/jpeg and never executed by the browser.
func validateImageUpload(file *multipart.FileHeader) error {
	if file.Size > maxImageBytes {
		return fmt.Errorf("file is %d bytes, maximum %d", file.Size, maxImageBytes)
	}
	if !allowedImageExt[strings.ToLower(filepath.Ext(file.Filename))] {
		return fmt.Errorf("unsupported file type (allowed: jpg, jpeg, png, webp, gif, bmp, heic)")
	}
	return nil
}

func handleCreateRun(uploadDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var ocrProvider string
		var ocrModel string
		var preserveOrder bool = true
		var imagePaths []string
		var ocrResults []*engine.OCRResult

		// runID is minted up front so the stored upload files can be namespaced under it below,
		// which is what keeps two runs that both upload "photo.jpg" from clobbering each other's
		// file on disk while both OCRResults keep pointing at the one surviving copy.
		//
		// The millisecond timestamp alone is not a unique key: two create-run requests landing in
		// the same millisecond used to collide on it, which made the second Save overwrite the first
		// run in the store and both runs' uploads write the same file. The random suffix makes the
		// id — and therefore the primary key and the namespaced file name — unique without giving up
		// the human-readable timestamp.
		var idRand [4]byte
		if _, err := rand.Read(idRand[:]); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to allocate run id"})
			return
		}
		runID := fmt.Sprintf("run_%d_%x", time.Now().UnixNano()/1e6, idRand[:])

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
				sampleDir := filepath.Join(cwd, "imgs", "1")
				for i, name := range req.Images {
					// The sample directory is tried first, then the upload directory; both
					// resolutions are containment-checked, so a name that escapes either is
					// refused rather than reaching the OCR providers' os.ReadFile.
					imgPath, err := resolveInDir(sampleDir, name)
					if err == nil {
						if _, statErr := os.Stat(imgPath); os.IsNotExist(statErr) {
							imgPath, err = resolveInDir(uploadDir, name)
						}
					}
					if err != nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Rejected image name: %v", err)})
						return
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
			// Multipart is the path untrusted bytes arrive on, so the guards live here: a bounded
			// count, a per-file size cap, an image-only extension allowlist, and a run-scoped
			// stored name. Browsers always send Content-Length, so rejecting oversized bodies up
			// front avoids buffering gigabytes to a temp file before the per-file check runs.
			if c.Request.ContentLength > maxUploadBytes {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": fmt.Sprintf("Upload too large: maximum %d bytes per request", maxUploadBytes)})
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes)
			form, err := c.MultipartForm()
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid form data or upload too large"})
				return
			}

			files := form.File["images"]
			if len(files) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "No image files uploaded"})
				return
			}
			if len(files) > maxUploadImages {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Too many images: maximum %d per run", maxUploadImages)})
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

			// Validate every file before writing any, so a rejected upload at index N does not
			// leave the files at 0..N-1 written to disk under a run that is then never created.
			for _, file := range files {
				if err := validateImageUpload(file); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Rejected upload %q: %v", file.Filename, err)})
					return
				}
			}

			for i, file := range files {
				// Store under a run-scoped name so concurrent or later runs cannot overwrite each
				// other's source material. The index disambiguates two files in the same run that
				// share a client filename. Go's multipart parser already reduces Filename to its
				// base, and resolveInDir re-checks containment, so the prefix keeps it a flat name.
				storedName := fmt.Sprintf("%s-%d-%s", runID, i, file.Filename)
				dst, err := resolveInDir(uploadDir, storedName)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Rejected upload name: %v", err)})
					return
				}
				if err := c.SaveUploadedFile(file, dst); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to save file %s: %v", file.Filename, err)})
					return
				}
				imagePaths = append(imagePaths, dst)
				ocrResults = append(ocrResults, &engine.OCRResult{
					ImageIndex: i + 1,
					ImageName:  storedName,
					ImagePath:  fmt.Sprintf("/uploads/%s", storedName),
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

		run := &engine.ConversionRun{
			ID:          runID,
			Title:       fmt.Sprintf("Vocat Run %s", time.Now().Format("01-02 15:04")),
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
			Status:      engine.RunStatusCreated,
			OCRProvider: ocrProvider,
			OCRModel:    ocrModel,
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

	if !run.TryClaim(engine.RunStatusOCRProgress) {
		c.JSON(http.StatusConflict, gin.H{
			"error":  "This run is already being processed. Wait for it to finish or fail before starting again.",
			"status": run.Status,
		})
		return
	}
	run.SetProgress(5)
	run.AddLog(fmt.Sprintf("🚀 OCR Processing Started with Engine '%s' (Progress: 5%%)", run.OCRProvider))
	store.Save(run)

	go func(r *engine.ConversionRun) {
		provider, err := resolveRunProvider(r)
		if err != nil || provider == nil {
			failRun(r, fmt.Errorf("OCR provider '%s' not found", r.OCRProvider))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		ctx = engine.WithOCRModel(ctx, r.OCRModel)

		if err := runOCRPhase(r, provider, ctx); err != nil {
			return
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

		if !run.TryClaim(engine.RunStatusMerging) {
			c.JSON(http.StatusConflict, gin.H{
				"error":  "This run is already being processed. Wait for it to finish or fail before starting again.",
				"status": run.Status,
			})
			return
		}
		run.SetProgress(75)
		run.AddLog("🔮 AI Structuring Engine Launched. Merging OCR Transcriptions... (Progress: 75%)")
		store.Save(run)

		go func(r *engine.ConversionRun) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			ctx = engine.WithOCRModel(ctx, r.OCRModel)
			mergedText := mergeOCRResults(r)
			imagePaths := buildImagePaths(r, uploadDir)

			words, err := structureRun(r, ctx, mergedText, imagePaths)
			if err != nil {
				failRun(r, fmt.Errorf("Conversion failed: %w", err))
				return
			}

			if err := writeRunOutputs(r, outputDir, words); err != nil {
				failRun(r, fmt.Errorf("Failed to write outputs: %w", err))
				return
			}

			r.SetStatus(engine.RunStatusCompleted)
			r.SetProgress(100)
			r.AddLog("🎉 Run Completed Successfully! Vocat JSON & DOC Test Sheets Ready (Progress: 100%)")
			store.Save(r)
		}(run)

		c.JSON(http.StatusOK, gin.H{"message": "Structuring started", "run": run})
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

		if !run.TryClaim(engine.RunStatusOCRProgress) {
			c.JSON(http.StatusConflict, gin.H{
				"error":  "This run is already being processed. Wait for it to finish or fail before starting again.",
				"status": run.Status,
			})
			return
		}
		run.SetProgress(5)
		run.AddLog(fmt.Sprintf("🚀 One-Click Conversion Engine Started/Retried with Provider '%s' (model: %s)", run.OCRProvider, run.OCRModel))
		store.Save(run)

		go func(r *engine.ConversionRun) {
			provider, err := resolveRunProvider(r)
			if err != nil || provider == nil {
				failRun(r, fmt.Errorf("OCR provider '%s' not found", r.OCRProvider))
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			ctx = engine.WithOCRModel(ctx, r.OCRModel)

			// Phase 1: Full OCR Recognition (5% -> 70%)
			if err := runOCRPhase(r, provider, ctx); err != nil {
				return
			}

			// Phase 2: Merge & AI Structuring (75% -> 95%)
			r.SetStatus(engine.RunStatusMerging)
			r.SetProgress(75)
			r.AddLog("🔮 AI Structuring Engine Launched. Merging Transcriptions... (75%)")
			store.Save(r)

			mergedText := mergeOCRResults(r)
			imagePaths := buildImagePaths(r, uploadDir)
			words, err := structureRun(r, ctx, mergedText, imagePaths)
			if err != nil {
				failRun(r, fmt.Errorf("Conversion failed: %w", err))
				return
			}

			// Phase 3: Export JSON & DOC Files (100%)
			if err := writeRunOutputs(r, outputDir, words); err != nil {
				failRun(r, fmt.Errorf("Failed to write outputs: %w", err))
				return
			}

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

		if err := engine.GenerateDocFile(run.Words, docPath, run.Title); err != nil {
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

func handleSendTelegram(outputDir string) gin.HandlerFunc {
	return func(c *gin.Context) {
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

		// Ensure document path is verified inside outputDir
		docFileName := fmt.Sprintf("%s.doc", run.ID)
		safeDocPath := filepath.Join(outputDir, docFileName)
		if !containedIn(outputDir, safeDocPath) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid document path"})
			return
		}

		caption := fmt.Sprintf("📚 Vocat Input Word Sheet (%s)\nWords: %d items\nProvider: %s", run.Title, len(run.Words), run.OCRProvider)
		sendName := engine.SanitizeFileName(run.Title)
		if err := engine.SendDocToTelegram(safeDocPath, sendName, caption); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Telegram delivery failed: %v", err)})
			return
		}

		run.AddLog("✈️ DOC Test Sheet Successfully Transmitted to Telegram!")
		store.Save(run)

		c.JSON(http.StatusOK, gin.H{"message": "Document successfully sent to Telegram", "run": run})
	}
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

		prevWords := run.Words
		run.SetWords(payload.Words)

		jsonFileName := fmt.Sprintf("%s.json", run.ID)
		docFileName := fmt.Sprintf("%s.doc", run.ID)
		jsonPath := filepath.Join(outputDir, jsonFileName)
		docPath := filepath.Join(outputDir, docFileName)

		jsonBytes, err := json.MarshalIndent(payload.Words, "", "  ")
		if err != nil {
			run.SetWords(prevWords)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to marshal words: %v", err)})
			return
		}

		prevJSONBytes, _ := os.ReadFile(jsonPath)

		if err := os.WriteFile(jsonPath, jsonBytes, 0o644); err != nil {
			run.SetWords(prevWords)
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to write JSON output: %v", err)})
			return
		}
		if err := engine.GenerateDocFile(payload.Words, docPath, run.Title); err != nil {
			run.SetWords(prevWords)
			if prevJSONBytes != nil {
				_ = os.WriteFile(jsonPath, prevJSONBytes, 0o644)
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to generate DOC file: %v", err)})
			return
		}

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
	run.SetTitle(strings.TrimSpace(payload.Title))
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

		// Delete from in-memory store and mark deleted FIRST to stop background workers
		store.Delete(id)

		// Delete uploaded images, but only files this server actually wrote. Unlinking a
		// stored path unconditionally made run deletion a way to remove any file the process
		// could reach, and it would also destroy source material under imgs/ that a run
		// merely referenced.
		for _, img := range run.Images {
			imgPath := img
			if !filepath.IsAbs(imgPath) {
				imgPath = filepath.Join(uploadDir, img)
			}
			if !containedIn(uploadDir, imgPath) {
				log.Printf("[delete run %s] skipping %s: outside the upload directory", id, imgPath)
				continue
			}
			_ = os.Remove(imgPath)
		}

		// Delete json & doc files
		_ = os.Remove(filepath.Join(outputDir, fmt.Sprintf("%s.json", id)))
		_ = os.Remove(filepath.Join(outputDir, fmt.Sprintf("%s.doc", id)))

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
	fileName = engine.SanitizeFileName(fileName)
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
	fileName = engine.SanitizeFileName(fileName)
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

type ModelOption struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Desc    string `json:"desc"`
	Default bool   `json:"default,omitempty"`
}

type ProviderOption struct {
	ID           string        `json:"id"`
	Label        string        `json:"label"`
	Desc         string        `json:"desc"`
	DefaultModel string        `json:"defaultModel"`
	Models       []ModelOption `json:"models"`
}

func handleGetModels(c *gin.Context) {
	forceRefresh := strings.ToLower(c.Query("refresh")) == "true"
	providers := getDynamicModels(c.Request.Context(), forceRefresh)

	c.JSON(http.StatusOK, gin.H{
		"providers": providers,
	})
}
