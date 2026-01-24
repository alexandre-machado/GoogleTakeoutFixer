package fixer

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

var sidecarSuffixes = []string{
	".supplemental-m.json",
	".supplemental-metadata.json",
	".supplemental-metada.json",
}

// Duplicate a file from one path to another
func CopyFile(inputPath string, outputPath string) error {
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
func FindSidecar(imagePath string) string {
	for _, suffix := range sidecarSuffixes {
		p := imagePath + suffix
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Checks if the file at the given path has the specified extension
func IsNameExtension(extension string, path string) bool {
	return strings.EqualFold(filepath.Ext(path), extension)
}
