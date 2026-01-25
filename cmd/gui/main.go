package main

import (
	"fmt"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
	"github.com/ncruces/zenity"
)

func main() {
	var inputPath, outputPath string

	// Create app / window
	a := app.New()
	w := a.NewWindow("GoogleTakeoutFixer")

	progressLabel := widget.NewLabel("")
	progressBar := widget.NewProgressBar()

	// Button for opening file dialog for choosing google takeout path and output path
	inputButton := widget.NewButton("Select Google Takeout Folder", func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Google Takeout Folder"), zenity.Directory())
		if err == nil {
			inputPath = dir
			fmt.Println("Input folder:", inputPath)
		}
	})

	outputButton := widget.NewButton("Select Output Folder", func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Output Folder"), zenity.Directory())
		if err == nil {
			outputPath = dir
			fmt.Println("Output folder:", outputPath)
		}
	})

	// Button to start processing
	var startButton *widget.Button
	startButton = widget.NewButton("Start Processing", func() {
		// one of the folders has not been selected
		if inputPath == "" || outputPath == "" {
			progressLabel.SetText("Select both in and output")
			return
		}

		// Disable buttons while processing
		inputButton.Disable()
		outputButton.Disable()
		startButton.Disable()
		progressLabel.SetText("Processing")
		progressBar.SetValue(0)

		progressCh := make(chan fixer.Progress)

		go func() {
			if err := fixer.Process(inputPath, outputPath, progressCh); err != nil {
				fyne.Do(func() {
					progressLabel.SetText("Error: " + err.Error())
				})
			}
		}()

		// Update progress
		go func() {
			for p := range progressCh {
				percentage := 0.0
				if p.Total > 0 {
					percentage = (float64(p.Processed) / float64(p.Total)) * 100.0
				}

				fyne.Do(func() {
					progressBar.Max = float64(p.Total)
					progressBar.SetValue(float64(p.Processed))
					progressLabel.SetText(fmt.Sprintf("[%.2f%%] %d/%d - %s",
						percentage, p.Processed, p.Total, filepath.Base(p.Current)))
				})
			}

			// Processing complete
			fyne.Do(func() {
				progressLabel.SetText("Done")
				progressBar.SetValue(progressBar.Max)
				inputButton.Enable()
				outputButton.Enable()
				startButton.Enable()
			})
		}()
	})

	w.SetContent(container.NewVBox(
		inputButton,
		outputButton,
		startButton,
		progressBar,
		progressLabel,
	))

	w.ShowAndRun()
}
