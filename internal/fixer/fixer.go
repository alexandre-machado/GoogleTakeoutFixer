package fixer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

type Progress struct {
	Total     int
	Processed int
	Current   string
}

// TODO: Add more options
// TODO: Disable checkboxes when processing
type ProcessOptions struct {
	UseSymlinks   bool
	WriteMetadata bool
}

type FixerContext struct {
	Ctx        context.Context
	SourceRoot string
	OutputRoot string
	Options    ProcessOptions
	ProgressCh chan<- Progress
}

// All media extension to differ between media files and other files
var mediaExtensions = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".heic": {},

	".mp4": {},
	".mov": {},
	".avi": {},
	".mkv": {},
}

// Process is the main fixer entry point.
// Process
// -> DiscoverDirs
// --> ProcessDirectory
// ---> ProcessFile
// TODO: Do something in case files already exists instead of overwriting them
func Process(
	ctx context.Context,
	sourcePath string,
	outputPath string,
	progressCh chan<- Progress,
	options ProcessOptions,
) error {
	err := InitializeFileLogger()
	if err != nil {
		if LogHandler != nil {
			LogHandler(LoggerWarn, fmt.Sprintf("Failed to initialize file logger: %v", err))
		}
	} else {
		defer func() {
			err := CloseFileLogger()
			if err != nil && LogHandler != nil {
				LogHandler(LoggerWarn, fmt.Sprintf("Failed to close file logger: %v", err))
			}
		}()
	}

	Log(LoggerInfo, "Starting processing with source: %s and output: %s", sourcePath, outputPath)

	// Log total processing time when processing is done
	startTime := time.Now()
	defer func() {
		Log(LoggerInfo, "Total processing time: %s", time.Since(startTime).Round(time.Second))
	}()

	defer close(progressCh)
	p := Progress{}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if options.WriteMetadata {
		if err := InitializeExifTool(); err != nil {
			Log(LoggerError, "Failed to initialize exiftool: %v", err)
			return err
		}
		defer CloseExifTool()
	}

	amountImages, err := CountProcessableFiles(sourcePath)
	if err != nil {
		Log(LoggerError, "Error counting images: %v", err)
		return err
	}
	p.Total = amountImages
	progressCh <- p

	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		Log(LoggerError, "Error getting file info: %v", err)
		return err
	}

	fixerCtx := &FixerContext{
		Ctx:        ctx,
		SourceRoot: sourcePath,
		OutputRoot: outputPath,
		Options:    options,
		ProgressCh: progressCh,
	}

	// process all directories in the source directory, ignore files in the source directory itself
	// because all media files should be inside of sub-folders
	if fileInfo.IsDir() {
		dirs, err := DiscoverDirs(sourcePath)
		if err != nil {
			Log(LoggerError, "Error discovering directories: %v", err)
		}

		for _, dir := range dirs {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			dirPath := filepath.Join(sourcePath, dir.Name())
			var targetPath string = filepath.Join(outputPath, dir.Name())
			p = ProcessDirectory(fixerCtx, dirPath, targetPath, p)
		}
	} else {
		err = ProcessFile(fixerCtx, sourcePath, outputPath)
		if err != nil {
			Log(LoggerError, "Error processing file: %v", err)
		} else {
			p.Processed++
			p.Current = sourcePath
			progressCh <- p
		}
	}

	return nil
}

// Process a directory and fix all files within the directory. Ignores sub-directories.
func ProcessDirectory(
	fixerCtx *FixerContext,
	dirPath string,
	outputPath string,
	p Progress,
) Progress {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		Log(LoggerError, "Error reading directory: %v", err)
		return p
	}

	// TODO: Fix potential race conditions
	// Job pools
	// Buffered channel to avoid blocking
	jobs := make(chan string, len(files))
	completed := make(chan string)
	// Channel to capture errors
	errors := make(chan error)

	var wg sync.WaitGroup
	workerCount := runtime.NumCPU() * 2 // x2 is faster for IO tasks, x more than that has no effect based on testing

	// Start worker goroutines
	for i := 0; i < workerCount; i++ {
		go func() {
			for imagePath := range jobs {
				if fixerCtx.Ctx.Err() != nil {
					wg.Done()
					continue
				}
				err := ProcessFile(fixerCtx, imagePath, outputPath)
				if err != nil {
					errors <- fmt.Errorf("error processing file %s: %w", imagePath, err)
				} else {
					completed <- imagePath
				}
				wg.Done()
			}
		}()
	}

	// Send jobs directly, add work group before transmitting job
	for _, file := range files {
		if fixerCtx.Ctx.Err() != nil {
			break
		}
		if file.IsDir() {
			continue
		}

		imagePath := filepath.Join(dirPath, file.Name())

		// Check whether a file is a media file
		if !IsMediaFile(imagePath) {
			continue
		}

		wg.Add(1)
		jobs <- imagePath
	}

	// All jobs have been sent
	close(jobs)

	// Close completed and errors channels when all jobs are finished
	go func() {
		wg.Wait()
		close(completed)
		close(errors)
	}()

	// Update progress and handle errors
	for {
		select {
		case ev, ok := <-completed:
			if !ok {
				completed = nil
			} else {
				p.Processed++
				p.Current = ev
				fixerCtx.ProgressCh <- p
			}
		case err, ok := <-errors:
			if !ok {
				errors = nil
			} else {
				Log(LoggerError, "%v", err)
			}
		case <-fixerCtx.Ctx.Done():
			// Let workers finish their current job but dont add new jobs
		}

		if completed == nil && errors == nil {
			break
		}
	}

	return p
}

// ProcessFile processes a single file by finding its sidecar file and then fixing it using the sidecar's metadata
// TODO: This function is written unorganized and should be refactored
func ProcessFile(
	fixerCtx *FixerContext,
	sourcePath string,
	outputPath string,
) error {
	fileName := filepath.Base(sourcePath)
	destPath := filepath.Join(outputPath, fileName)

	if _, err := os.Stat(destPath); err == nil {
		Log(LoggerInfo, "File %s already exists, skipping", destPath)
		return nil
	}

	sidecarPath, err := FindSidecar(sourcePath)

	if err != nil {
		Log(LoggerError, "Error finding sidecar for file %s: %v", sourcePath, err)
		return err
	}

	// Metadata sidecar file not found
	if sidecarPath == "" {
		Log(LoggerWarn, "No sidecar file found for %s", sourcePath)
		return nil
	}

	err = CreateFixedFile(fixerCtx, sourcePath, sidecarPath, destPath)
	if err != nil {
		Log(LoggerError, "Error creating fixed file for %s: %v", sourcePath, err)
		return err
	}

	return nil
}

func CreateFixedFile(
	fixerCtx *FixerContext,
	filePath string,
	fileMetadataPath string,
	destPath string,
) error {
	// Ensure output directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return err
	}

	fileName := filepath.Base(destPath)

	isYearFolder, _ := IsYearFolder(filepath.Base(filepath.Dir(destPath)))

	if fixerCtx.Options.UseSymlinks && !isYearFolder {
		// Attempt to find the file inside of any year folder in the output
		// TODO: Make this more efficient, whole output directory is being searched every time
		entries, _ := os.ReadDir(fixerCtx.OutputRoot)
		for _, curEntry := range entries {
			if !curEntry.IsDir() {
				continue
			}

			isYear, _ := IsYearFolder(curEntry.Name())
			if !isYear {
				continue
			}

			target := filepath.Join(fixerCtx.OutputRoot, curEntry.Name(), fileName)
			if _, err := os.Stat(target); err == nil {
				if err := os.Symlink(target, destPath); err != nil {
					// Symlink failed, continue with normal copy
					if !os.IsExist(err) {
						return fmt.Errorf("Failed to create symlink: %w", err)
					}
				} else {
					// Symlink successful
					return nil
				}
			}
		}
	}

	if err := DuplicateFile(filePath, destPath); err != nil {
		return err
	}

	if fixerCtx.Options.WriteMetadata {
		metadata, err := ReadJsonMetadata(fileMetadataPath)
		if err != nil {
			Log(LoggerError, "Failed to read metadata from %s: %v", fileMetadataPath, err)
		} else {
			// Only apply metadata if reading was successful
			err = ApplyMetadata(destPath, metadata)
			if err != nil {
				Log(LoggerError, "Failed to apply metadata to %s: %v", destPath, err)
			}
		}
	}

	return nil
}
