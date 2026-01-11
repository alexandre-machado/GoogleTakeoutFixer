package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type imageMetadata struct {
	Title          string `json:"title"`
	PhotoTakenTime struct {
		Timestamp string `json:"timestamp"`
	} `json:"photoTakenTime"`
}

func ProcessTakeout(inputPath string, outputPath string) {
	var allFolders []os.DirEntry = FindDirs(inputPath)
	var yearFolders, albumFolders = FindYearAlbumFolders(allFolders)

	CreateFixedImageFolder(inputPath, outputPath, yearFolders, albumFolders)
}

func CreateFixedImageFolder(baseInputPath string, outputFolder string, yearFolders []os.DirEntry, albumFolders []os.DirEntry) {
	outputDir := filepath.Join(outputFolder, "output")
	if err := os.Mkdir(outputDir, os.ModePerm); err != nil {
		fmt.Println(err)
	}

	fmt.Printf("%v %v\n", yearFolders, albumFolders)
	fmt.Printf("Output folder: %v\n", outputFolder)

	for _, curYearDir := range yearFolders {
		if !curYearDir.IsDir() {
			fmt.Println("File in YearFolder is not a directory!  ", curYearDir.Name())
			continue
		}

		yearPath := filepath.Join(baseInputPath, curYearDir.Name())
		fmt.Printf("Reading year directory: %s\n", yearPath)

		files := ReadDirectory(yearPath)
		ProcessFiles(files, yearPath, outputFolder)
	}
}

func ReadDirectory(path string) []os.DirEntry {
	entries, err := os.ReadDir(path)
	if err != nil {
		fmt.Printf("Error reading directory %s: %v\n", path, err)
		return nil
	}
	return entries
}

func ProcessFiles(files []os.DirEntry, basePath string, outputFolder string) {
	for _, entry := range files {
		filePath := filepath.Join(basePath, entry.Name())

		if entry.IsDir() {
			fmt.Printf("Found album sub-directory: %s\n", filePath)
			continue
		}

		if IsNameExtension(".json", entry.Name()) {
			continue
		}

		fmt.Printf("Found file: %s\n", filePath)
		fmt.Println("File name:  ", entry.Name())

		outputPath := filepath.Join(outputFolder, entry.Name())

		if HasSidecarFile(filePath, ".supplemental-m.json") {
			DuplicateAndFixImage(filePath, outputPath, ".supplemental-m.json")
		} else if HasSidecarFile(filePath, ".supplemental-metadata.json") {
			DuplicateAndFixImage(filePath, outputPath, ".supplemental-metadata.json")
		} else {
			fmt.Println("no image metadata json found for " + filePath)
		}
	}
}

func DuplicateAndFixImage(filePath string, outputPath string, metdataExtension string) {
	if err := DuplicateFile(filePath, outputPath); err != nil {
		fmt.Printf("Error while duplicating: %v\n", err)
		return
	}

	jsonPath := filePath + metdataExtension
	meta, err := ReadMetadata(jsonPath)
	if err != nil {
		fmt.Printf("Error reading metadata(%s): %v\n", jsonPath, err)
		return
	}

	if err := ApplyFileTime(outputPath, meta); err != nil {
		fmt.Printf("Error setting timestamp: %v\n", err)
	} else {
		fmt.Printf("Image fixed: %s\n", outputPath)
	}
}

func ApplyFileTime(filePath string, meta imageMetadata) error {
	timestampInt, err := strconv.ParseInt(meta.PhotoTakenTime.Timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %v", err)
	}

	newTime := time.Unix(timestampInt, 0)
	return os.Chtimes(filePath, newTime, newTime)
}

func ReadMetadata(jsonPath string) (imageMetadata, error) {
	var data imageMetadata

	jsonFile, err := os.Open(jsonPath)
	if err != nil {
		return data, err
	}
	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return data, err
	}

	err = json.Unmarshal(byteValue, &data)
	return data, err
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
