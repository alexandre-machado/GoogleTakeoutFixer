package gui

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

func Main() {
	var inputPath, outputPath string

	// Create app / window
	a := app.New()
	a.SetIcon(resourceGoogleTakeoutFixerPng)
	w := a.NewWindow("GoogleTakeoutFixer")
	w.Resize(fyne.NewSize(550, 400))

	var useSymlinks bool = false
	var writeExif bool = false

	progressLabel := widget.NewLabel("")
	progressLabel.Truncation = fyne.TextTruncateEllipsis
	progressBar := widget.NewProgressBar()

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
	//inputButton.Resize(fyne.NewSize(200, inputButton.MinSize().Height))

	var outputButton *widget.Button
	outputButton = widget.NewButton("Select Output Folder", func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Output Folder"), zenity.Directory())
		if err == nil {
			outputPath = dir
			outputButton.SetText("Output: " + filepath.Base(outputPath))
			fmt.Println("Output folder:", outputPath)
		}
	})
	//outputButton.Resize(fyne.NewSize(200, outputButton.MinSize().Height))

	// Checkboxes for options. Default value is false
	UseLinksCheckbox := widget.NewCheck("Use symlinks for albums", func(value bool) {
		useSymlinks = value
		fmt.Println("use symlinks", useSymlinks)
	})

	WriteExifCheckbox := widget.NewCheck("Write EXIF metadata", func(value bool) {
		writeExif = value
		fmt.Println("write exif", writeExif)
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
			if err := fixer.Process(inputPath, outputPath, progressCh, useSymlinks, writeExif); err != nil {
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

	logEntry := widget.NewMultiLineEntry()
	logEntry.Disable()

	fixer.LogHandler = func(level fixer.LogLevel, message string) {
		logMsg := fmt.Sprintf("[%s] %s\n", level, message)
		fyne.Do(func() {
			logEntry.SetText(logEntry.Text + logMsg)
			logEntry.Refresh()
		})
	}

	fixer.LogHandler = func(level fixer.LogLevel, message string) {
		logMsg := fmt.Sprintf("[%s] %s\n", level, message)
		fyne.Do(func() {
			logEntry.SetText(logEntry.Text + logMsg)
			logEntry.Refresh()
		})
	}

	logEntry.SetText(logEntry.Text + "Logs will appear here...\n")

	FolderButtons := container.NewGridWithColumns(
		2,
		inputButton,
		outputButton,
	)

	CheckBoxes := container.NewVBox(
		UseLinksCheckbox,
		WriteExifCheckbox,
	)

	SecondRow := container.NewGridWithColumns(
		2,
		CheckBoxes,
		startButton,
	)

	topContent := container.NewVBox(
		FolderButtons,
		SecondRow,

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
