package cli

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

func Main() {
	if len(os.Args) < 3 {
		fmt.Println("Flags missing! Enter InputPath and OutputPath.")
		return
	}

	inputPath := os.Args[1]
	outputPath := os.Args[2]

	progressCh := make(chan fixer.Progress)

	go func() {
		if err := fixer.Process(inputPath, outputPath, progressCh); err != nil {
			fmt.Println("error:", err)
		}
	}()

	// Receive progress event
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
