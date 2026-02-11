package cli

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

func Main() {
	// Handle logs from the fixer package by printing them
	fixer.LogHandler = func(level fixer.LogLevel, message string) {
		fmt.Printf("[%s] %s\n", level, message)
	}

	// Command-line flags
	inputPath := flag.String("input", "", "Path to Google takeout directory")
	outputPath := flag.String("output", "", "Path to output directory")
	useSymlinks := flag.Bool("symlink", false, "Use symlinks inside of albums instead of duplicating images")
	skipMetadata := flag.Bool("skip-metadata", false, "Skip writing metadata to files")

	flag.Parse()

	if *inputPath == "" || *outputPath == "" {
		fmt.Println("Error: --input and --output are required")
		flag.Usage()
		os.Exit(1)
	}

	progressCh := make(chan fixer.Progress)

	options := fixer.ProcessOptions{UseSymlinks: *useSymlinks, WriteMetadata: !*skipMetadata}
	go func() {
		// Invert skipMetadata because the flag is named skipMetadata but the process function expects writeMetadata
		if err := fixer.Process(context.Background(), *inputPath, *outputPath, progressCh, options); err != nil {
			fmt.Printf("Error during processing: %v\n", err)
		}
	}()

	for p := range progressCh {
		if p.Processed == 0 {
			continue
		}

		percentageFinished := math.Round(float64(p.Processed) / float64(p.Total) * 100)

		fmt.Printf("[%3.0f%%] %2d/%2d - %s\n",
			percentageFinished,
			p.Processed,
			p.Total,
			filepath.Base(p.Current),
		)
	}

	fmt.Println("\nDone")
}
