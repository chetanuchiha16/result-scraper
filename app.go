package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"result-scraper/internal/scraper"
	"result-scraper/internal/zipper"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ─── Shared progress event name ───────────────────────────────────────────────
const evtProgress = "scraper:progress"
const evtCaptcha = "scraper:captcha"
const evtDone = "scraper:done"
const evtLog = "scraper:log"

// ─── Progress payload emitted to the frontend ─────────────────────────────────
type ProgressPayload struct {
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Failed  int    `json:"failed"`
	Current string `json:"current"`
}

// ─── App struct ───────────────────────────────────────────────────────────────
type App struct {
	ctx context.Context

	mu           sync.Mutex
	session      *scraper.Session
	captchaCh    chan string // frontend sends solved CAPTCHA text here
	downloadDir  string
	zipPath      string
	totalUSNs    int
	doneUSNs     int
	failedUSNs   int
	currentUSN   string
	batchRunning bool
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.captchaCh = make(chan string, 1)
}

func (a *App) shutdown(_ context.Context) {
	if a.session != nil {
		a.session.Close()
	}
}

// ─── StartBatch ───────────────────────────────────────────────────────────────
// Called by the React frontend to kick off a batch scraping run.
func (a *App) StartBatch(examSession, examYear, usnPrefix string, start, end int) error {
	a.mu.Lock()
	if a.batchRunning {
		a.mu.Unlock()
		return fmt.Errorf("a batch is already running")
	}
	a.batchRunning = true
	a.doneUSNs = 0
	a.failedUSNs = 0
	a.totalUSNs = end - start + 1
	a.mu.Unlock()

	// Create a temp directory for this batch's PDFs
	dir, err := os.MkdirTemp("", "vtu-scraper-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	a.downloadDir = dir
	a.zipPath = ""

	go a.runBatch(examSession, examYear, usnPrefix, start, end)
	return nil
}

// ─── SubmitCaptcha ────────────────────────────────────────────────────────────
// Called by frontend when user types the CAPTCHA solution.
func (a *App) SubmitCaptcha(text string) {
	select {
	case a.captchaCh <- text:
	default:
	}
}

// ─── GetZipPath ───────────────────────────────────────────────────────────────
func (a *App) GetZipPath() string {
	return a.zipPath
}

// ─── SaveZip ──────────────────────────────────────────────────────────────────
// Opens a save-file dialog so the user can choose where to store the zip.
func (a *App) SaveZip() (string, error) {
	if a.zipPath == "" {
		return "", fmt.Errorf("no zip file available yet")
	}
	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		DefaultFilename: filepath.Base(a.zipPath),
		Filters: []runtime.FileFilter{
			{DisplayName: "ZIP Archive", Pattern: "*.zip"},
		},
	})
	if err != nil || dest == "" {
		return "", err
	}
	data, err := os.ReadFile(a.zipPath)
	if err != nil {
		return "", err
	}
	return dest, os.WriteFile(dest, data, 0644)
}

// ─── runBatch ─────────────────────────────────────────────────────────────────
func (a *App) runBatch(examSession, examYear, usnPrefix string, start, end int) {
	defer func() {
		a.mu.Lock()
		a.batchRunning = false
		a.mu.Unlock()
	}()

	sess := scraper.NewSession()
	a.session = sess
	baseURL := fmt.Sprintf("https://results.vtu.ac.in/%scbcs%s/index.php", examSession, examYear)

	for i := start; i <= end; i++ {
		usn := fmt.Sprintf("%s%03d", usnPrefix, i)
		a.mu.Lock()
		a.currentUSN = usn
		a.mu.Unlock()

		a.emitProgress()
		a.emitLog("info", fmt.Sprintf("Fetching CAPTCHA for %s…", usn))

		// 1. Get CAPTCHA image bytes
		imgBytes, err := sess.FetchCaptcha(baseURL, usn)
		if err != nil {
			a.emitLog("error", fmt.Sprintf("[%s] CAPTCHA fetch failed: %v", usn, err))
			a.mu.Lock()
			a.failedUSNs++
			a.mu.Unlock()
			a.emitProgress()
			continue
		}

		// 2. Send image to frontend and wait for user to type solution
		runtime.EventsEmit(a.ctx, evtCaptcha, map[string]interface{}{
			"usn":     usn,
			"imgB64":  imgBytes, // already base64 encoded inside FetchCaptcha
		})
		captchaText := <-a.captchaCh

		// 3. Submit form and fetch result HTML
		resultHTML, err := sess.SubmitCaptcha(usn, captchaText)
		if err != nil {
			a.emitLog("error", fmt.Sprintf("[%s] Submission failed: %v", usn, err))
			a.mu.Lock()
			a.failedUSNs++
			a.mu.Unlock()
			a.emitProgress()
			continue
		}

		// 4. Render HTML to PDF via chromedp
		pdfPath := filepath.Join(a.downloadDir, usn+".pdf")
		if err := scraper.RenderHTMLToPDF(resultHTML, pdfPath); err != nil {
			a.emitLog("error", fmt.Sprintf("[%s] PDF generation failed: %v", usn, err))
			a.mu.Lock()
			a.failedUSNs++
			a.mu.Unlock()
			a.emitProgress()
			continue
		}

		a.mu.Lock()
		a.doneUSNs++
		a.mu.Unlock()
		a.emitLog("success", fmt.Sprintf("[%s] ✓ PDF saved", usn))
		a.emitProgress()
	}

	// 5. Build ZIP
	a.emitLog("info", "Packaging PDFs into zip archive…")
	zipOut := filepath.Join(os.TempDir(), fmt.Sprintf("vtu-%s%s-results.zip", examSession, examYear))
	if err := zipper.Archive(a.downloadDir, zipOut); err != nil {
		a.emitLog("error", fmt.Sprintf("ZIP creation failed: %v", err))
		return
	}
	a.zipPath = zipOut
	a.emitLog("success", fmt.Sprintf("ZIP ready: %s", zipOut))

	a.mu.Lock()
	a.currentUSN = ""
	a.mu.Unlock()
	a.emitProgress()
	runtime.EventsEmit(a.ctx, evtDone, map[string]interface{}{
		"zipPath": zipOut,
		"done":    a.doneUSNs,
		"failed":  a.failedUSNs,
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────────
func (a *App) emitProgress() {
	a.mu.Lock()
	p := ProgressPayload{
		Done:    a.doneUSNs,
		Total:   a.totalUSNs,
		Failed:  a.failedUSNs,
		Current: a.currentUSN,
	}
	a.mu.Unlock()
	runtime.EventsEmit(a.ctx, evtProgress, p)
}

func (a *App) emitLog(level, msg string) {
	runtime.EventsEmit(a.ctx, evtLog, map[string]interface{}{
		"level": level,
		"msg":   msg,
	})
}
