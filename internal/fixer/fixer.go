package fixer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Process is the main fixer entry point.
// Process
// -> DiscoverDirs
// --> ProcessDirectory
// ---> ProcessFile
func Process(sourcePath string, outputPath string) error {
	dirs, err := DiscoverDirs(sourcePath)
	if err != nil {
		fmt.Println("error discovering: ", err)
	}

	fmt.Println(dirs)

	err = ProcessFile(sourcePath, outputPath)
	if err != nil {
		fmt.Println(err)
	}

	for _, dir := range dirs {

		dirPath := string(sourcePath) + /*string(os.PathSeparator) + */ dir.Name()
		fmt.Println(dirPath)

		var targetPath string = outputPath + dir.Name()

		ProcessDirectory(dirPath, targetPath)

		isYear, err := CheckWhetherYear(dir.Name())

		if err != nil {
			fmt.Println(err)
		}

		fmt.Println(dir.Name(), ":", isYear)
	}

	return nil
}

// Process a directory and fix all files within the directory. Ignores sub-directories.
func ProcessDirectory(dirPath string, outputPath string) error {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		imagePath := filepath.Join(dirPath, file.Name())

		if file.IsDir() {
			fmt.Println("file is a dir")
			continue
		}

		// Check whether a file is an image or not
		// TODO: Add support for any image/video file without hard coding
		if !IsNameExtension(".jpg", imagePath) && !IsNameExtension(".png", imagePath) {
			continue
		}

		ProcessFile(imagePath, outputPath)

	}

	return nil
}

// ProcessFile processes a single file by finding its sidecar file and then fixing it using the sidecar's metadata
func ProcessFile(sourcePath string, outputPath string) error {
	sidecarPath := FindSidecar(sourcePath)

	// Metadata sidecar file not found
	if sidecarPath == "" {
		return nil
	}

	fmt.Println(sidecarPath)

	meta, err := ReadJsonMetadata(sidecarPath)
	if err != nil {
		fmt.Println("error reading metadata: ", err)
	}

	CreateFixedFile(sourcePath, sidecarPath, outputPath)
	fmt.Println(sourcePath, sidecarPath, outputPath)

	fmt.Println(meta.PhotoTakenTime)

	return nil
}

// Create a fixed file with fixed metadata
func CreateFixedFile(filePath string, fileMetadataPath string, outputPath string) error {
	// Ensure output directory exists
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return err
	}

	fileName := filepath.Base(filePath)
	destPath := filepath.Join(outputPath, fileName)

	if err := CopyFile(filePath, destPath); err != nil {
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
