package fixer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Duplicate a file from one path to another
func DuplicateFile(inputPath string, outputPath string) error {
	sourceFile, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// Discover directories within a path non recursively
func DiscoverDirs(path string) ([]os.DirEntry, error) {
	var dirList []os.DirEntry

	files, err := os.ReadDir(path)

	if err != nil {
		return nil, err
	}

	for _, file := range files {
		if file.IsDir() {
			dirList = append(dirList, file)
		}
	}

	return dirList, nil
}

// Find a matching sidecar JSON
func FindSidecar(imagePath string) (string, error) {
	// Example: photoname.jpg.supplemental-metadata.json
	pattern := imagePath + ".supplemental-*.json"

	matches, err := filepath.Glob(pattern)
	if err == nil && len(matches) > 0 {
		return matches[0], nil
	}

	return "", nil
}

// Checks if the file at the given path has the specified extension
func IsNameExtension(extension string, path string) bool {
	return strings.EqualFold(filepath.Ext(path), extension)
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
	extension := strings.ToLower(filepath.Ext(path))
	_, isImage := imageExtensions[extension]
	_, isVideo := videoExtensions[extension]
	return isImage || isVideo
}

// Attempts to find an image file with the same base name as the video file
// This is used for live photos where the metadata is the images sidecar
// I think error handling could be improved here
func FindImagePartner(videoPath string) (string, error) {
	if !IsVideoFile(videoPath) {
		return "", nil
	}

	dir := filepath.Dir(videoPath)
	extension := filepath.Ext(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), extension)

	// Check all image extensions for a match
	for imgExt := range imageExtensions {
		candidate := filepath.Join(dir, base+imgExt)

		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		// Try uppercase extension
		candidateUpper := filepath.Join(dir, base+strings.ToUpper(imgExt))
		if _, err := os.Stat(candidateUpper); err == nil {
			return candidateUpper, nil
		}
	}

	return "", nil
}

// Counts all processable files in the source path
func CountProcessableFiles(sourcePath string) (int, error) {
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return 0, err
	}

	if !fileInfo.IsDir() {
		return 0, fmt.Errorf("source path is not a directory")
	}

	count := 0
	subdirs, err := DiscoverDirs(sourcePath)
	if err != nil {
		return 0, err
	}

	for _, dir := range subdirs {
		files, _ := os.ReadDir(filepath.Join(sourcePath, dir.Name()))
		for _, file := range files {
			if !file.IsDir() && IsMediaFile(file.Name()) {
				count++
			}
		}
	}

	if count == 0 {
		return 0, fmt.Errorf("no media files found in folder structure")
	}
	return count, nil
}
