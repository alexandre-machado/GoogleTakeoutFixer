package gui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
	"github.com/ncruces/zenity"
)

func Main() {
	var inputPath, outputPath string

	// Create app / window
	a := app.New()
	a.SetIcon(resourceGoogleTakeoutFixerPng)
	w := a.NewWindow("GoogleTakeoutFixer")
	w.Resize(fyne.NewSize(550, 400))

	var useSymlinks bool = false
	var writeMetadata bool = false

	progressLabel := widget.NewLabel("")
	progressLabel.Truncation = fyne.TextTruncateEllipsis
	progressBar := widget.NewProgressBar()
	var cancelFn context.CancelFunc
	var cancelButton *widget.Button

	// Button for opening file dialog for choosing google takeout path and output path
	var inputButton *widget.Button
	inputButton = widget.NewButton("Select Google Takeout Folder", func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Google Takeout Folder"), zenity.Directory())
		if err == nil {
			inputPath = dir
			inputButton.SetText("Input: " + filepath.Base(inputPath))
			fmt.Println("Input folder:", inputPath)
		}
	})

	var outputButton *widget.Button
	outputButton = widget.NewButton("Select Output Folder", func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Output Folder"), zenity.Directory())
		if err == nil {
			outputPath = dir
			outputButton.SetText("Output: " + filepath.Base(outputPath))
			fmt.Println("Output folder:", outputPath)
		}
	})

	// Checkboxes for options. Default value is false
	useLinksCheckbox := widget.NewCheck("Use symlinks for albums", func(value bool) {
		useSymlinks = value
		fmt.Println("use symlinks", useSymlinks)
	})

	writeMetadataCheckbox := widget.NewCheck("Write metadata", func(value bool) {
		writeMetadata = value
		fmt.Println("write metadata", writeMetadata)
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

		ctx, cancel := context.WithCancel(context.Background())
		cancelFn = cancel
		cancelButton.Enable()

		progressCh := make(chan fixer.Progress)

		opts := fixer.ProcessOptions{UseSymlinks: useSymlinks, WriteMetadata: writeMetadata}
		go func() {
			if err := fixer.Process(ctx, inputPath, outputPath, progressCh, opts); err != nil {
				if ctx.Err() == nil {
					fyne.Do(func() {
						progressLabel.SetText("Error: " + err.Error())
					})
				}
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
				if ctx.Err() != nil {
					progressLabel.SetText("Cancelled")
				} else {
					progressLabel.SetText("Done")
					progressBar.SetValue(progressBar.Max)
				}
				cancelButton.Disable()
				cancelFn = nil
				inputButton.Enable()
				outputButton.Enable()
				startButton.Enable()
			})
		}()
	})

	cancelButton = widget.NewButton("Cancel", func() {
		if cancelFn == nil {
			return
		}
		progressLabel.SetText("Cancelling...")
		cancelButton.Disable()
		cancelFn()
	})
	cancelButton.Disable()

	logEntry := widget.NewMultiLineEntry()
	logEntry.Disable()
	const maxVisibleLogLines = 200
	visibleLogLines := make([]string, 0, maxVisibleLogLines)

	fixer.LogHandler = func(level fixer.LogLevel, message string) {
		logMsg := fmt.Sprintf("[%s] %s", level, message)
		fyne.Do(func() {
			visibleLogLines = append(visibleLogLines, logMsg)
			if len(visibleLogLines) > maxVisibleLogLines {
				visibleLogLines = visibleLogLines[len(visibleLogLines)-maxVisibleLogLines:]
			}

			logEntry.SetText(strings.Join(visibleLogLines, "\n") + "\n")
			logEntry.Refresh()
		})
	}

	visibleLogLines = append(visibleLogLines, "Logs will appear here...")
	logEntry.SetText("Logs will appear here...\n")

	folderButtons := container.NewGridWithColumns(
		2,
		inputButton,
		outputButton,
	)

	checkBoxes := container.NewVBox(
		useLinksCheckbox,
		writeMetadataCheckbox,
	)

	secondRow := container.NewGridWithColumns(
		2,
		checkBoxes,
		container.NewGridWithColumns(2, startButton, cancelButton),
	)

	topContent := container.NewVBox(
		folderButtons,
		secondRow,

		progressLabel,
		progressBar,
	)

	w.SetContent(container.NewBorder(
		topContent,
		nil,
		nil,
		nil,
		logEntry, // expand
	))

	w.ShowAndRun()
}
