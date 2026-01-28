package fixer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
)

type Progress struct {
	Total     int
	Processed int
	Current   string
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

	err = ProcessFile(sourcePath, outputPath, sourcePath, outputPath, useSymlinks)
	if err != nil {
		fmt.Println(err)
	}

	for _, dir := range dirs {

		dirPath := filepath.Join(sourcePath, dir.Name())

		var targetPath string = filepath.Join(outputPath, dir.Name())

		p = ProcessDirectory(dirPath, targetPath, sourcePath, outputPath, useSymlinks, p, progressCh)
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
	p Progress,
	progressCh chan<- Progress,
) Progress {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return p
	}

	// Job pools
	// TODO: Fix potential race conditions
	jobs := make(chan string)
	completed := make(chan string)

	var wg sync.WaitGroup

	workerCount := runtime.NumCPU()

	// Start worker goroutines
	for i := 0; i < workerCount; i++ {
		go func() {
			for imagePath := range jobs {
				ProcessFile(imagePath, outputPath, sourcePath, rootOutputPath, useSymlinks)
				// signal completion for one job
				completed <- imagePath
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

		// Check whether a file is an image or not
		// TODO: Add support for any image/video file without hard coding
		// TODO: This check also happens within CountImagesRecursively, turn this into a function
		if !IsNameExtension(".jpg", imagePath) && !IsNameExtension(".png", imagePath) {
			continue
		}

		wg.Add(1)
		jobs <- imagePath
	}

	// All jobs have been sent
	close(jobs)

	// Close completed when all jobs are finished
	go func() {
		wg.Wait()
		close(completed)
	}()

	// Update progress using a goroutine
	for ev := range completed {
		p.Processed++
		p.Current = ev
		progressCh <- p
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
) error {
	sidecarPath := FindSidecar(sourcePath)

	// Metadata sidecar file not found
	if sidecarPath == "" {
		return nil
	}

	_, err := ReadJsonMetadata(sidecarPath)
	if err != nil {
		fmt.Println("error reading metadata: ", err)
	}

	CreateFixedFile(sourcePath, sidecarPath, outputPath, rootOutputPath, useSymlinks)

	return nil
}

func CreateFixedFile(
	filePath string,
	fileMetadataPath string,
	outputPath string,
	rootOutputPath string,
	useSymlinks bool,
) error {
	// Ensure output directory exists
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return err
	}

	fileName := filepath.Base(filePath)
	destPath := filepath.Join(outputPath, fileName)

	isYearFolder, _ := CheckWhetherYear(filepath.Base(outputPath))

	if useSymlinks && !isYearFolder {
		// Attempt to find the file inside of any year folder in the output
		// TODO: Make this more efficient, whole output directory is being searched every time
		entries, _ := os.ReadDir(rootOutputPath)
		for _, curEntry := range entries {
			if !curEntry.IsDir() {
				continue
			}

			isYear, _ := CheckWhetherYear(curEntry.Name())
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

	metadata, err := ReadJsonMetadata(fileMetadataPath)
	if err != nil {
		return err
	}

	ApplyFileTime(destPath, metadata)

	return nil
}

// Checks whether a directory is a standart google year folder
func CheckWhetherYear(dirPath string) (bool, error) {
	// TODO: Add support for non english takeouts
	re := regexp.MustCompile(`^Photos from \d+$`)

	if re.MatchString(dirPath) {
		return true, nil
	} else {
		return false, nil
	}
}
