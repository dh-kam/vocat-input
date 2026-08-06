package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vocat-input/internal/engine"
)

// The three conversion entry points — handleStartOCR (OCR only), handleMergeAndConvert (structure
// only) and handleOneClickConvert (the full pipeline) — used to carry their own copies of the OCR
// loop, the merge/structure/export block, provider lookup and model-env wiring. The copies had
// already drifted in their log wording, and a fix to one was a fix the other two would not get.
// These helpers are the single implementation of each shared step; the handlers now only own the
// bits that actually differ between them (which status they claim, whether they run synchronously,
// and their opening line).

// resolveRunProvider looks up the OCR provider a run was configured with.
func resolveRunProvider(r *engine.ConversionRun) (engine.OCRProvider, error) {
	return registry.Get(r.OCRProvider)
}

// applyModelEnv exports the run's model selection into the env vars the providers read. Setting a
// process-wide env var from a per-request field is coarse, but it is how the providers are wired
// today and the server is single-user.
func applyModelEnv(r *engine.ConversionRun) {
	if r.OCRModel == "" {
		return
	}
	if r.OCRProvider == "bedrock" {
		os.Setenv("BEDROCK_MODEL", r.OCRModel)
	} else {
		os.Setenv("VERTEX_MODEL", r.OCRModel)
	}
}

// failRun marks a run FAILED, records the error in its state and log, and persists it. Every
// terminal failure in the pipeline goes through here so the shape cannot drift between handlers.
func failRun(r *engine.ConversionRun, err error) {
	r.SetStatus(engine.RunStatusFailed)
	r.SetError(err.Error())
	r.AddLog(fmt.Sprintf("❌ %v", err))
	store.Save(r)
}

// ocrSnippetLog renders a one-line preview of a raw OCR response, capped and flattened, for the
// log stream so a run's output can be eyeballed against the structured result.
func ocrSnippetLog(index int, text string) string {
	snippet := strings.TrimSpace(text)
	if len(snippet) > 180 {
		snippet = snippet[:180] + "..."
	}
	snippet = strings.ReplaceAll(snippet, "\n", " ")
	return fmt.Sprintf("  📄 [RAW OCR RESPONSE #%d]: \"%s\"", index, snippet)
}

// runOCRPhase runs OCR over every image in r.Images, recording each result and advancing progress
// from 5% to 70%. On the first failure it marks the run FAILED and persisted, and returns the
// error so the caller can stop the pipeline.
func runOCRPhase(r *engine.ConversionRun, provider engine.OCRProvider, ctx context.Context) error {
	total := len(r.Images)
	for i, imgPath := range r.Images {
		currentProg := 5 + int((float64(i)/float64(total))*65.0)
		r.SetOCRResultStatus(i, "PROCESSING")
		r.SetProgress(currentProg)
		r.AddLog(fmt.Sprintf("📷 [%d/%d] OCR Vision Processing '%s' (model: %s)... (%d%%)", i+1, total, r.OCRResults[i].ImageName, r.OCRModel, currentProg))
		store.Save(r)

		text, err := provider.ProcessImage(ctx, imgPath)
		if err != nil {
			r.SetOCRResultStatus(i, "FAILED")
			r.SetOCRResultError(i, err.Error())
			failRun(r, fmt.Errorf("OCR Failed on '%s' [%d/%d]: %w", r.OCRResults[i].ImageName, i+1, total, err))
			return err
		}

		doneProg := 5 + int((float64(i+1)/float64(total))*65.0)
		r.SetOCRResultStatus(i, "COMPLETED")
		r.SetOCRResultText(i, text)
		r.SetProgress(doneProg)
		r.AddLog(fmt.Sprintf("✅ [%d/%d] OCR Completed on '%s' (%d%%)", i+1, total, r.OCRResults[i].ImageName, doneProg))
		r.AddLog(ocrSnippetLog(i+1, text))
		store.Save(r)
	}
	return nil
}

// mergeOCRResults joins the non-empty OCR transcriptions that the structuring stage will consume,
// stores the merged text on the run, and logs its length. The one-click path used to skip the log;
// both paths now report it so the log stream is consistent regardless of entry point.
func mergeOCRResults(r *engine.ConversionRun) string {
	var merged []string
	for _, res := range r.OCRResults {
		if res.RawText != "" {
			merged = append(merged, res.RawText)
		}
	}
	mergedText := strings.Join(merged, "\n\n")
	r.SetMergedText(mergedText)
	r.AddLog(fmt.Sprintf("📄 Merged OCR Text Prepared (%d total characters)", len(mergedText)))
	return mergedText
}

// buildImagePaths resolves run.Images — which may be absolute or upload-relative — into paths the
// structuring stage can hand to os.ReadFile inside the providers.
func buildImagePaths(r *engine.ConversionRun, uploadDir string) []string {
	var paths []string
	for _, img := range r.Images {
		if filepath.IsAbs(img) {
			paths = append(paths, img)
		} else {
			paths = append(paths, filepath.Join(uploadDir, img))
		}
	}
	return paths
}

// structureRun runs the two-stage AI structuring over the merged OCR text and stores the extracted
// words on the run, advancing progress 80% -> 95%. It returns the words so the caller can export
// them.
func structureRun(r *engine.ConversionRun, ctx context.Context, mergedText string, imagePaths []string) ([]engine.WordItem, error) {
	r.SetProgress(80)
	r.AddLog(fmt.Sprintf("🔍 Stage 1: Analyzing image format with AI Vision (%d images)... (80%%)", len(imagePaths)))
	store.Save(r)

	r.SetProgress(85)
	r.AddLog("🤖 Stage 2: Extracting vocabulary with format-aware AI prompt... (85%%)")
	words, err := engine.ConvertOCRToVocatJSON(ctx, mergedText, r.PreserveOrder, imagePaths, r.OCRProvider, r.OCRModel)
	if err != nil {
		return nil, err
	}
	r.SetWords(words)
	r.SetProgress(95)
	r.AddLog(fmt.Sprintf("✨ AI Structuring Completed! %d Structured Words Extracted (95%%)", len(words)))
	return words, nil
}

// writeRunOutputs serializes the words to JSON and DOC and records the output paths on the run.
func writeRunOutputs(r *engine.ConversionRun, outputDir string, words []engine.WordItem) {
	jsonFileName := fmt.Sprintf("%s.json", r.ID)
	docFileName := fmt.Sprintf("%s.doc", r.ID)
	jsonBytes, _ := json.MarshalIndent(words, "", "  ")
	_ = os.WriteFile(filepath.Join(outputDir, jsonFileName), jsonBytes, 0o644)
	_ = engine.GenerateDocFile(words, filepath.Join(outputDir, docFileName))
	r.SetOutputPaths(fmt.Sprintf("/outputs/%s", jsonFileName), fmt.Sprintf("/outputs/%s", docFileName))
}
