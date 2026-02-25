/*
GoogleTakeoutFixer - A tool to easily clean and organize Google Photos Takeout exports
Copyright (C) 2026 feloex

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package gui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
	version "github.com/feloex/GoogleTakeoutFixer/internal/version"
	"github.com/ncruces/zenity"
)

func Main() {
	var inputPath, outputPath string

	// Create app / window
	a := app.New()
	a.SetIcon(resourceGoogleTakeoutFixerPng)
	w := a.NewWindow("GoogleTakeoutFixer " + version.Tag)
	w.Resize(fyne.NewSize(550, 400))

	var useSymlinks bool = false
	var writeMetadata bool = false
	var flatten bool = false
	var ignoreAlbums bool = false
	var monthSubfolders bool = false

	progressLabel := widget.NewLabel("")
	progressLabel.Truncation = fyne.TextTruncateEllipsis
	progressBar := widget.NewProgressBar()
	var cancelFn context.CancelFunc
	var cancelButton *widget.Button

	// Button for opening file dialog for choosing google takeout path and output path
	var inputButton *widget.Button
	inputButton = widget.NewButtonWithIcon("Select Google Takeout Folder", theme.FolderOpenIcon(), func() {
		dir, err := zenity.SelectFile(zenity.Title("Select Google Takeout Folder"), zenity.Directory())
		if err == nil {
			inputPath = dir
			inputButton.SetText("Input: " + filepath.Base(inputPath))
			fmt.Println("Input folder:", inputPath)
		}
	})

	var outputButton *widget.Button
	outputButton = widget.NewButtonWithIcon("Select Output Folder", theme.FolderOpenIcon(), func() {
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

	ignoreAlbumsCheckbox := widget.NewCheck("Ignore album folders", func(value bool) {
		ignoreAlbums = value
		fmt.Println("ignore albums", ignoreAlbums)
	})

	monthSubfoldersCheckbox := widget.NewCheck("Create month subfolders", func(value bool) {
		monthSubfolders = value
		fmt.Println("month subfolders", monthSubfolders)
	})

	flattenCheckbox := widget.NewCheck("Flatten album structure", func(value bool) {
		flatten = value
		fmt.Println("flatten", flatten)
	})

	// Fix conflicting options
	updateCheckboxStates := func() {
		setEnabled := func(cb *widget.Check, enabled bool) {
			if enabled {
				cb.Enable()
			} else {
				cb.Disable()
			}
		}
		setEnabled(useLinksCheckbox, !ignoreAlbums && !flatten)
		setEnabled(ignoreAlbumsCheckbox, !useSymlinks && !flatten)
		setEnabled(flattenCheckbox, !useSymlinks && !ignoreAlbums && !monthSubfolders)
		setEnabled(monthSubfoldersCheckbox, !flatten)
	}

	for _, cb := range []*widget.Check{useLinksCheckbox, ignoreAlbumsCheckbox, flattenCheckbox, monthSubfoldersCheckbox} {
		cb := cb
		prev := cb.OnChanged
		cb.OnChanged = func(v bool) {
			prev(v)
			updateCheckboxStates()
		}
	}

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

		opts := fixer.ProcessOptions{
			UseSymlinks:     useSymlinks,
			WriteMetadata:   writeMetadata,
			Flatten:         flatten,
			IgnoreAlbums:    ignoreAlbums,
			MonthSubfolders: monthSubfolders,
		}
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
	const maxVisibleLogLines = 200
	visibleLogLines := make([]string, 0, maxVisibleLogLines)

	// Prevent user from editing the log while keeping text selectable
	// This is not optimal but fyne does not provide a better way to do this
	var logUpdating bool
	logEntry.OnChanged = func(_ string) {
		if logUpdating {
			return
		}
		logUpdating = true
		logEntry.SetText(strings.Join(visibleLogLines, "\n") + "\n")
		logUpdating = false
	}

	fixer.LogHandler = func(level fixer.LogLevel, message string) {
		logMsg := fmt.Sprintf("[%s] %s", level, message)
		fyne.Do(func() {
			visibleLogLines = append(visibleLogLines, logMsg)
			if len(visibleLogLines) > maxVisibleLogLines {
				visibleLogLines = visibleLogLines[len(visibleLogLines)-maxVisibleLogLines:]
			}

			logUpdating = true
			logEntry.SetText(strings.Join(visibleLogLines, "\n") + "\n")
			logUpdating = false
			logEntry.CursorRow = len(visibleLogLines)
			logEntry.CursorColumn = 0
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

	CheckBoxRow := container.NewGridWithColumns(
		2,
		useLinksCheckbox,
		writeMetadataCheckbox,
		ignoreAlbumsCheckbox,
		monthSubfoldersCheckbox,
		flattenCheckbox,
	)

	StartCancelRow := container.NewGridWithColumns(2, startButton, cancelButton)

	topContent := container.NewVBox(
		folderButtons,
		CheckBoxRow,
		StartCancelRow,
		progressBar,
		progressLabel,
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
