package main

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

var InputPath string
var OutputPath string

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Flags missing! Enter InputPath (path of your takeout) and OutputPath (where your fixed files will be located).")
		return
	}

	InputPath = os.Args[1]
	OutputPath = os.Args[2]

	processTakeout(InputPath, OutputPath)
}

func processTakeout(inputPath string, outputPath string) {
	var allFolders []os.DirEntry = findDirs(inputPath)
	var yearFolders, albumFolders = findYearAlbumFolders(allFolders)

	createFixedImageFolder(inputPath, outputPath, yearFolders, albumFolders)
}

func createFixedImageFolder(baseInputPath string, outputFolder string, yearFolders []os.DirEntry, albumFolders []os.DirEntry) {
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

		files := readDirectory(yearPath)
		processFiles(files, yearPath, outputFolder)
	}
}

func readDirectory(path string) []os.DirEntry {
	entries, err := os.ReadDir(path)
	if err != nil {
		fmt.Printf("Error reading directory %s: %v\n", path, err)
		return nil
	}
	return entries
}

func processFiles(files []os.DirEntry, basePath string, outputFolder string) {
	for _, entry := range files {
		filePath := filepath.Join(basePath, entry.Name())

		if entry.IsDir() {
			fmt.Printf("Found album sub-directory: %s\n", filePath)
			continue
		}

		if isNameExtension(".json", entry.Name()) {
			continue
		}

		fmt.Printf("Found file: %s\n", filePath)
		fmt.Println("File name:  ", entry.Name())

		outputPath := filepath.Join(outputFolder, entry.Name())

		if hasSidecarFile(filePath, ".supplemental-m.json") {
			duplicateAndFixImage(filePath, outputPath, ".supplemental-m.json")
		} else if hasSidecarFile(filePath, ".supplemental-metadata.json") {
			duplicateAndFixImage(filePath, outputPath, ".supplemental-metadata.json")
		} else {
			fmt.Println("no image metadata json found for " + filePath)
		}
	}
}

func duplicateAndFixImage(filePath string, outputPath string, metdataExtension string) {
	if err := duplicateFile(filePath, outputPath); err != nil {
		fmt.Printf("Error while duplicating: %v\n", err)
		return
	}

	jsonPath := filePath + metdataExtension
	meta, err := readMetadata(jsonPath)
	if err != nil {
		fmt.Printf("Error reading metadata(%s): %v\n", jsonPath, err)
		return
	}

	if err := applyFileTime(outputPath, meta); err != nil {
		fmt.Printf("Error setting timestamp: %v\n", err)
	} else {
		fmt.Printf("Image fixed: %s\n", outputPath)
	}
}

func applyFileTime(filePath string, meta imageMetadata) error {
	timestampInt, err := strconv.ParseInt(meta.PhotoTakenTime.Timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %v", err)
	}

	newTime := time.Unix(timestampInt, 0)
	return os.Chtimes(filePath, newTime, newTime)
}

func readMetadata(jsonPath string) (imageMetadata, error) {
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

func duplicateFile(source string, destination string) error {
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

func findYearAlbumFolders(albums []os.DirEntry) ([]os.DirEntry, []os.DirEntry) {
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

func findDirs(path string) []os.DirEntry {
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

func hasSidecarFile(originalPath string, suffix string) bool {
	sidecarPath := originalPath + suffix
	_, err := os.Stat(sidecarPath)
	return err == nil
}

func isNameExtension(extension string, path string) bool {
	return strings.EqualFold(filepath.Ext(path), extension)
}
