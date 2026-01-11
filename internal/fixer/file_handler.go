package fixer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func ReadDirectory(path string) []os.DirEntry {
	entries, err := os.ReadDir(path)
	if err != nil {
		fmt.Printf("Error reading directory %s: %v\n", path, err)
		return nil
	}
	return entries
}

func DuplicateFile(source string, destination string) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func FindYearAlbumFolders(albums []os.DirEntry) ([]os.DirEntry, []os.DirEntry) {
	// todo: add language support
	re := regexp.MustCompile(`^Photos from \d+$`)

	var yearFolders []os.DirEntry
	var albumFolders []os.DirEntry

	for _, entry := range albums {
		_, err := entry.Info()
		if err == nil {
			if re.MatchString(entry.Name()) {
				yearFolders = append(yearFolders, entry)
			} else {
				albumFolders = append(albumFolders, entry)
			}
		}
	}

	return yearFolders, albumFolders
}

func FindDirs(path string) []os.DirEntry {
	var dirlist []os.DirEntry

	files, err := os.ReadDir(path)

	if err != nil {
		fmt.Println("Error: " + err.Error())
		return dirlist
	}

	for _, file := range files {
		if file.IsDir() {
			dirlist = append(dirlist, file)
		}
	}

	return dirlist
}

func HasSidecarFile(originalPath string, suffix string) bool {
	sidecarPath := originalPath + suffix
	_, err := os.Stat(sidecarPath)
	return err == nil
}

func IsNameExtension(extension string, path string) bool {
	return strings.EqualFold(filepath.Ext(path), extension)
}
