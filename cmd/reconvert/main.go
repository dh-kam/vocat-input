// Command reconvert regenerates the AI-structured words for an existing run.
//
// It exists to repair runs whose stored bounding boxes were corrupted by an engine bug that has
// since been fixed (the structuring stage now normalizes every box to a 0-100 percent grid). It
// reuses the OCR text already stored on the run, so it spends one structuring API call rather than
// re-running OCR, and it never touches the source images.
//
// Two subcommands keep the expensive step separable from the destructive one:
//
//	reconvert generate --run <id> [--db path] [--provider p] [--model m] [--out-words path]
//	    Calls engine.ConvertOCRToVocatJSON on the run's stored OCR text, prints before/after
//	    bounding-box health, and writes the fresh words to --out-words. The DB is not modified.
//
//	reconvert apply --run <id> --db <path> --outputs <dir> --words <path> [--confirm]
//	    Replaces the run's words with the ones from --words, marks bboxScale 100, regenerates
//	    the <outputs>/<id>.json and .doc files, and writes the DB. Backs up the DB and the old
//	    outputs first. Without --confirm it prints the planned change and exits.
//
// Run from the repo root so internal/engine can read ./.env for credentials.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vocat-input/internal/engine"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "generate":
		cmdGenerate(os.Args[2:])
	case "apply":
		cmdApply(os.Args[2:])
	default:
		usage()
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: reconvert {generate|apply} [flags]")
}

const defaultDB = "storage/runs_db.json"

// loadRuns reads the on-disk run list without reordering it, so an apply writes back a near-identical
// file (only the target run changes) instead of churning every record.
func loadRuns(dbPath string) ([]*engine.ConversionRun, error) {
	data, err := os.ReadFile(dbPath)
	if err != nil {
		return nil, err
	}
	var runs []*engine.ConversionRun
	if err := json.Unmarshal(data, &runs); err != nil {
		return nil, fmt.Errorf("parse %s: %w", dbPath, err)
	}
	return runs, nil
}

func findRun(runs []*engine.ConversionRun, id string) (*engine.ConversionRun, int) {
	for i, r := range runs {
		if r.ID == id {
			return r, i
		}
	}
	return nil, -1
}

func cmdGenerate(args []string) {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	runID := fs.String("run", "", "run id to re-convert (required)")
	dbPath := fs.String("db", defaultDB, "path to runs_db.json")
	provider := fs.String("provider", "", "override OCR provider (default: run's)")
	model := fs.String("model", "", "override OCR model (default: run's)")
	outWords := fs.String("out-words", "", "write fresh words here (default: /tmp/<id>_fresh_words.json)")
	_ = fs.Parse(args)

	if *runID == "" {
		fs.Usage()
		os.Exit(2)
	}

	runs, err := loadRuns(*dbPath)
	must(err)
	run, _ := findRun(runs, *runID)
	if run == nil {
		exitf("run %q not found in %s", *runID, *dbPath)
	}

	fmt.Printf("=== run %s ===\n", run.ID)
	fmt.Printf("provider=%s model=%s preserveOrder=%v images=%d\n", run.OCRProvider, run.OCRModel, run.PreserveOrder, len(run.Images))
	fmt.Printf("BEFORE: %s\n", bboxHealth(run.Words))

	// Reuse the OCR text already on the run; re-running OCR would only spend money to reproduce it.
	var merged []string
	for _, res := range run.OCRResults {
		if res.RawText != "" {
			merged = append(merged, res.RawText)
		}
	}
	if len(merged) == 0 {
		exitf("run has no stored OCR text to re-structure")
	}
	mergedText := joinNonEmpty(merged, "\n\n")

	p := run.OCRProvider
	if *provider != "" {
		p = *provider
	}
	m := run.OCRModel
	if *model != "" {
		m = *model
	}

	fmt.Printf("calling engine.ConvertOCRToVocatJSON (provider=%s model=%s)…\n", p, m)
	result, err := engine.ConvertOCRToVocatJSON(context.Background(), mergedText, run.PreserveOrder, run.Images, p, m)
	if err != nil {
		exitf("structuring failed: %v", err)
	}
	words := result.Words
	fmt.Printf("AFTER:  %s (Title: %q)\n", bboxHealth(words), result.Title)

	out := *outWords
	if out == "" {
		out = fmt.Sprintf("/tmp/%s_fresh_words.json", run.ID)
	}
	b, err := json.MarshalIndent(words, "", "  ")
	must(err)
	must(os.WriteFile(out, b, 0o644))
	fmt.Printf("wrote %d fresh words to %s\n", len(words), out)
}

func cmdApply(args []string) {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	runID := fs.String("run", "", "run id to update (required)")
	dbPath := fs.String("db", defaultDB, "path to runs_db.json")
	outputs := fs.String("outputs", "storage/outputs", "output directory for <id>.json/.doc")
	wordsPath := fs.String("words", "", "path to fresh words JSON (required)")
	confirm := fs.Bool("confirm", false, "actually write; without this the command is a dry run")
	_ = fs.Parse(args)

	if *runID == "" || *wordsPath == "" {
		fs.Usage()
		os.Exit(2)
	}

	b, err := os.ReadFile(*wordsPath)
	must(err)
	var words []engine.WordItem
	must(json.Unmarshal(b, &words))

	runs, err := loadRuns(*dbPath)
	must(err)
	run, _ := findRun(runs, *runID)
	if run == nil {
		exitf("run %q not found in %s", *runID, *dbPath)
	}

	fmt.Printf("=== apply to %s ===\n", *dbPath)
	fmt.Printf("BEFORE: %s\n", bboxHealth(run.Words))
	fmt.Printf("AFTER:  %s\n", bboxHealth(words))

	if !*confirm {
		fmt.Println("dry run — pass --confirm to write")
		return
	}

	// Back up the DB and the old outputs so the overwrite is reversible.
	stamp := time.Now().Format("20060102-150405")
	must(copyFile(*dbPath, *dbPath+".bak-"+stamp))
	for _, ext := range []string{".json", ".doc"} {
		old := filepath.Join(*outputs, run.ID+ext)
		if _, err := os.Stat(old); err == nil {
			must(copyFile(old, old+".bak-"+stamp))
		}
	}

	run.Words = words
	run.BBoxScale = engine.BBoxOutputScale // fresh boxes leave the engine on the 0-100 grid
	run.Status = engine.RunStatusCompleted
	run.Progress = 100
	run.Error = ""
	run.UpdatedAt = time.Now()
	run.JSONPath = fmt.Sprintf("/outputs/%s.json", run.ID)
	run.DocPath = fmt.Sprintf("/outputs/%s.doc", run.ID)

	// Regenerate the export files from the fresh words.
	wordsJSON, err := json.MarshalIndent(words, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(*outputs, run.ID+".json"), wordsJSON, 0o644))
	must(engine.GenerateDocFile(words, filepath.Join(*outputs, run.ID+".doc")))

	// Preserve order; the store writes CreatedAt-desc and the file is already in that order.
	out, err := json.MarshalIndent(runs, "", "  ")
	must(err)
	must(os.WriteFile(*dbPath, out, 0o644))
	fmt.Printf("wrote %s + outputs (backups saved with .bak-%s)\n", *dbPath, stamp)
}

// bboxHealth summarises whether a word list's bounding boxes are on the expected 0-100 grid.
// "degenerate" counts slivers that are almost certainly placeholders; "big" counts coordinates
// above the percent ceiling, i.e. still on the old 0-1000 (or pixel) scale.
func bboxHealth(words []engine.WordItem) string {
	if len(words) == 0 {
		return "no words"
	}
	degen, big, maxc := 0, 0, 0
	for _, w := range words {
		m := 0
		for _, v := range w.BBox {
			if v > m {
				m = v
			}
		}
		if m > maxc {
			maxc = m
		}
		if m > 0 && m <= 10 {
			degen++
		}
		if m > engine.BBoxOutputScale {
			big++
		}
	}
	return fmt.Sprintf("%d words, max coord=%d, degenerate(<=10)=%d, over-100=%d", len(words), maxc, degen, big)
}

func joinNonEmpty(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i > 0 && out != "" {
			out += sep
		}
		out += p
	}
	return out
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o644)
}

func must(err error) {
	if err != nil {
		exitf("%v", err)
	}
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
