package fixer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

type Progress struct {
	Total     int
	Processed int
	Current   string
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
	sourcePath string,
	outputPath string,
	progressCh chan<- Progress,
	useSymlinks bool,
	writeMetadata bool,
) error {
	defer close(progressCh)
	p := Progress{}

	amountImages, err := CountImagesRecursive(sourcePath)
	if err != nil {
		fmt.Println("Error counting images:", err)
		return err
	}
	p.Total = amountImages
	progressCh <- p

	dirs, err := DiscoverDirs(sourcePath)
	if err != nil {
		fmt.Println("error discovering: ", err)
	}

	err = ProcessFile(sourcePath, outputPath, sourcePath, outputPath, useSymlinks, writeMetadata)
	if err != nil {
		fmt.Println(err)
	}

	for _, dir := range dirs {

		dirPath := filepath.Join(sourcePath, dir.Name())

		var targetPath string = filepath.Join(outputPath, dir.Name())

		p = ProcessDirectory(dirPath, targetPath, sourcePath, outputPath, useSymlinks, writeMetadata, p, progressCh)
	}

	return nil
}

// Process a directory and fix all files within the directory. Ignores sub-directories.
func ProcessDirectory(
	dirPath string,
	outputPath string,
	sourcePath string,
	rootOutputPath string,
	useSymlinks bool,
	writeMetadata bool,
	p Progress,
	progressCh chan<- Progress,
) Progress {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		fmt.Printf("Error reading directory %s: %v\n", dirPath, err)
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
				err := ProcessFile(imagePath, outputPath, sourcePath, rootOutputPath, useSymlinks, writeMetadata)
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
				progressCh <- p
			}
		case err, ok := <-errors:
			if !ok {
				errors = nil
			} else {
				fmt.Printf("Error: %v\n", err)
			}
		}

		if completed == nil && errors == nil {
			break
		}
	}

	return p
}

// ProcessFile processes a single file by finding its sidecar file and then fixing it using the sidecar's metadata
func ProcessFile(
	sourcePath string,
	outputPath string,
	rootSourcePath string,
	rootOutputPath string,
	useSymlinks bool,
	writeMetadata bool,
) error {
	sidecarPath := FindSidecar(sourcePath)

	// Metadata sidecar file not found
	if sidecarPath == "" {
		return nil
	}

	if writeMetadata {
		_, err := ReadJsonMetadata(sidecarPath)
		if err != nil {
			fmt.Println("error reading metadata: ", err)
		}
	}

	CreateFixedFile(sourcePath, sidecarPath, outputPath, rootOutputPath, useSymlinks, writeMetadata)

	return nil
}

func CreateFixedFile(
	filePath string,
	fileMetadataPath string,
	outputPath string,
	rootOutputPath string,
	useSymlinks bool,
	writeMetadata bool,
) error {
	// Ensure output directory exists
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return err
	}

	fileName := filepath.Base(filePath)
	destPath := filepath.Join(outputPath, fileName)

	isYearFolder, _ := IsYearFolder(filepath.Base(outputPath))

	if useSymlinks && !isYearFolder {
		// Attempt to find the file inside of any year folder in the output
		// TODO: Make this more efficient, whole output directory is being searched every time
		entries, _ := os.ReadDir(rootOutputPath)
		for _, curEntry := range entries {
			if !curEntry.IsDir() {
				continue
			}

			isYear, _ := IsYearFolder(curEntry.Name())
			if !isYear {
				continue
			}

			target := filepath.Join(rootOutputPath, curEntry.Name(), fileName)
			if _, err := os.Stat(target); err == nil {
				if err := os.Symlink(target, destPath); err != nil {
					// Symlink failed, continue with normal copy
					if !os.IsExist(err) {
						return fmt.Errorf("failed to create symlink: %w", err)
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

	if writeMetadata {
		metadata, err := ReadJsonMetadata(fileMetadataPath)
		if err != nil {
			return err
		}

		ApplyMetadata(destPath, metadata)
	}

	return nil
}

// Checks whether a directory is a standart google year folder
func IsYearFolder(dirPath string) (bool, error) {
	// Year folder prefixes of some countries
	yearPrefixes := []string{
		"Photos from ", // English
		"Fotos von ",   // German
		"Photos de ",   // French
		"Foto del ",    // Italian
		"Fotos de ",    // Spanish / Portuguese
	}

	for _, prefix := range yearPrefixes {
		if strings.HasPrefix(dirPath, prefix) {
			// The rest of the string has to be 4 characters long
			yearPart := strings.TrimPrefix(dirPath, prefix)
			if matched, _ := regexp.MatchString(`^\d{4}$`, yearPart); matched {
				return true, nil
			}
		}
	}
	return false, nil
}

// Checks whether a file, that is provided using its path, is a media file
func IsMediaFile(path string) bool {
	extension := filepath.Ext(path)
	_, ok := mediaExtensions[strings.ToLower(extension)]
	return ok
}
