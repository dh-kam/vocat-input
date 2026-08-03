package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"vocat-input/internal/engine"

	"github.com/dh-kam/refutils/flagsbinder"
	"github.com/spf13/cobra"
)

type ProcessOptions struct {
	Dir           string `flag:"dir" default:"" usage:"directory containing vocabulary images"`
	Provider      string `flag:"provider" default:"fallback" usage:"OCR provider engine"`
	Providers     string `flag:"providers" default:"" usage:"comma-separated list of OCR engines for multi-chain processing (e.g. vertex,cursor)"`
	DoubleCheck   bool   `flag:"double-check" default:"false" usage:"enable 2-step multi-engine OCR double check & cross-verification"`
	OutDoc        string `flag:"out-doc" default:"vocat_output.doc" usage:"output .doc test sheet file path"`
	OutJSON       string `flag:"out-json" default:"vocat_output.json" usage:"output .json file path"`
	PreserveOrder bool   `flag:"preserve-order" default:"true" usage:"preserve word sequence numbers"`
	OCRProvider   string `flag:"ocr-provider" default:"bedrock" usage:"AI Structuring provider"`
	OCRModel      string `flag:"ocr-model" default:"us.anthropic.claude-sonnet-4-6" usage:"AI Structuring model"`
}

type OCROptions struct {
	Image    string `flag:"image" default:"" usage:"image file path to process OCR"`
	Provider string `flag:"provider" default:"fallback" usage:"OCR provider engine"`
}

type ConvertOptions struct {
	InputText string `flag:"text" default:"" usage:"raw OCR text"`
	InputFile string `flag:"file" default:"" usage:"raw OCR text file path"`
	OutDoc    string `flag:"out-doc" default:"vocat_output.doc" usage:"output .doc file path"`
	OutJSON   string `flag:"out-json" default:"vocat_output.json" usage:"output .json file path"`
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rootCmd := &cobra.Command{
		Use:           "vocat-cli",
		Short:         "vocat-cli is a CLI tool for vocabulary OCR processing, AI structuring, and .doc sheet generation",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(
		newProcessCmd(),
		newOCRCmd(),
		newConvertCmd(),
	)

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

func newProcessCmd() *cobra.Command {
	var opts ProcessOptions
	binder := flagsbinder.NewViperCobraFlagsBinder().
		String("dir", "", "directory containing vocabulary images").
		String("provider", "fallback", "OCR provider engine").
		String("providers", "", "comma-separated list of OCR engines for multi-chain processing (e.g. vertex,cursor)").
		Bool("double-check", false, "enable 2-step multi-engine OCR double check & cross-verification").
		String("out-doc", "vocat_output.doc", "output .doc test sheet file path").
		String("out-json", "vocat_output.json", "output .json file path").
		Bool("preserve-order", true, "preserve word sequence numbers").
		String("ocr-provider", "bedrock", "AI Structuring provider").
		String("ocr-model", "us.anthropic.claude-sonnet-4-6", "AI Structuring model")

	cmd := &cobra.Command{
		Use:           "process",
		Short:         "Run end-to-end OCR processing, text merging, AI structuring, and DOC sheet generation",
		SilenceUsage:  true,
		SilenceErrors: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return binder.BindCommand(cmd, &opts, args...)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.Providers) != "" {
				opts.Provider = opts.Providers
			} else if opts.DoubleCheck {
				opts.Provider = "doublecheck"
			}
			return runProcess(cmd.Context(), opts)
		},
	}
	binder.SetTo(cmd.Flags())
	return cmd
}

func runProcess(ctx context.Context, opts ProcessOptions) error {
	if strings.TrimSpace(opts.Dir) == "" {
		return errors.New("--dir flag is required (e.g. --dir /workspace/vocat-input2/imgs/1)")
	}

	entries, err := os.ReadDir(opts.Dir)
	if err != nil {
		return fmt.Errorf("read directory %s: %w", opts.Dir, err)
	}

	var imagePaths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" {
			imagePaths = append(imagePaths, filepath.Join(opts.Dir, entry.Name()))
		}
	}

	if len(imagePaths) == 0 {
		return fmt.Errorf("no image files (.jpg, .png, .webp) found in %s", opts.Dir)
	}

	sort.Strings(imagePaths)
	fmt.Printf("\033[1;36m[vocat-cli]\033[0m Processing %d images from %s using provider '\033[1;33m%s\033[0m'...\n", len(imagePaths), opts.Dir, opts.Provider)

	registry := engine.NewProviderRegistry()
	provider, err := registry.Get(opts.Provider)
	if err != nil {
		return fmt.Errorf("\033[1;31m❌ [ERROR] OCR provider '%s' not found: %v\033[0m", opts.Provider, err)
	}

	var ocrTexts []string
	for i, imgPath := range imagePaths {
		fmt.Printf("  \033[36m[%d/%d]\033[0m Running OCR on \033[1m%s\033[0m...\n", i+1, len(imagePaths), filepath.Base(imgPath))
		text, err := provider.ProcessImage(ctx, imgPath)
		if err != nil {
			return fmt.Errorf("\033[1;31m❌ [ERROR] OCR Failed on %s: %v\033[0m", filepath.Base(imgPath), err)
		}
		ocrTexts = append(ocrTexts, text)
	}

	mergedText := strings.Join(ocrTexts, "\n\n")
	fmt.Println("\033[1;36m[vocat-cli]\033[0m Merging OCR texts and running AI structuring...")

	words, err := engine.ConvertOCRToVocatJSON(ctx, mergedText, opts.PreserveOrder, imagePaths, opts.OCRProvider, opts.OCRModel)
	if err != nil {
		return fmt.Errorf("\033[1;31m❌ [ERROR] Convert OCR to JSON failed: %v\033[0m", err)
	}

	jsonBytes, err := json.MarshalIndent(words, "", "  ")
	if err != nil {
		return fmt.Errorf("\033[1;31m❌ [ERROR] Marshal JSON failed: %v\033[0m", err)
	}

	if err := os.WriteFile(opts.OutJSON, jsonBytes, 0o644); err != nil {
		return fmt.Errorf("\033[1;31m❌ [ERROR] Write JSON file failed: %v\033[0m", err)
	}
	fmt.Printf("\033[1;32m[vocat-cli] ✅ JSON output saved to: %s (%d words)\033[0m\n", opts.OutJSON, len(words))

	if err := engine.GenerateDocFile(words, opts.OutDoc); err != nil {
		return fmt.Errorf("\033[1;31m❌ [ERROR] Generate DOC file failed: %v\033[0m", err)
	}
	fmt.Printf("\033[1;32m[vocat-cli] ✅ DOC test sheet saved to: %s\033[0m\n", opts.OutDoc)

	fmt.Println("\033[1;32m🎉 [vocat-cli] Processing successfully completed!\033[0m")
	return nil
}

func newOCRCmd() *cobra.Command {
	var opts OCROptions
	binder := flagsbinder.NewViperCobraFlagsBinder().
		String("image", "", "image file path to process OCR").
		String("provider", "fallback", "OCR provider engine")

	cmd := &cobra.Command{
		Use:           "ocr",
		Short:         "Run OCR on a single image and output raw text",
		SilenceUsage:  true,
		SilenceErrors: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return binder.BindCommand(cmd, &opts, args...)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOCR(cmd.Context(), opts)
		},
	}
	binder.SetTo(cmd.Flags())
	return cmd
}

func runOCR(ctx context.Context, opts OCROptions) error {
	if strings.TrimSpace(opts.Image) == "" {
		return errors.New("--image flag is required")
	}

	registry := engine.NewProviderRegistry()
	provider, err := registry.Get(opts.Provider)
	if err != nil {
		return err
	}

	text, err := provider.ProcessImage(ctx, opts.Image)
	if err != nil {
		return err
	}

	fmt.Println(text)
	return nil
}

func newConvertCmd() *cobra.Command {
	var opts ConvertOptions
	binder := flagsbinder.NewViperCobraFlagsBinder().
		String("text", "", "raw OCR text").
		String("file", "", "raw OCR text file path").
		String("out-doc", "vocat_output.doc", "output .doc file path").
		String("out-json", "vocat_output.json", "output .json file path")

	cmd := &cobra.Command{
		Use:           "convert",
		Short:         "Convert raw OCR text into Vocat JSON and .doc test sheet",
		SilenceUsage:  true,
		SilenceErrors: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return binder.BindCommand(cmd, &opts, args...)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConvert(cmd.Context(), opts)
		},
	}
	binder.SetTo(cmd.Flags())
	return cmd
}

func runConvert(ctx context.Context, opts ConvertOptions) error {
	var text string
	if strings.TrimSpace(opts.InputText) != "" {
		text = opts.InputText
	} else if opts.InputFile != "" {
		raw, err := os.ReadFile(opts.InputFile)
		if err != nil {
			return fmt.Errorf("read file %s: %w", opts.InputFile, err)
		}
		text = string(raw)
	} else {
		return errors.New("one of --text or --file is required")
	}

	words, err := engine.ConvertOCRToVocatJSON(ctx, text, true, nil, "doublecheck", "")
	if err != nil {
		return err
	}

	jsonBytes, _ := json.MarshalIndent(words, "", "  ")
	_ = os.WriteFile(opts.OutJSON, jsonBytes, 0o644)
	_ = engine.GenerateDocFile(words, opts.OutDoc)

	fmt.Printf("[vocat-cli] Saved %s and %s (%d words)\n", opts.OutJSON, opts.OutDoc, len(words))
	return nil
}
