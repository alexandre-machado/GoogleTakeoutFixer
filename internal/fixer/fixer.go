package fixer

import (
	"fmt"
	"os"
	"path/filepath"
)

func ProcessTakeout(inputPath string, outputPath string) {
	var allFolders []os.DirEntry = FindDirs(inputPath)
	var yearFolders, albumFolders = FindYearAlbumFolders(allFolders)

	CreateFixedImageFolder(inputPath, outputPath, yearFolders, albumFolders)
}

func CreateFixedImageFolder(baseInputPath string, outputFolder string, yearFolders []os.DirEntry, albumFolders []os.DirEntry) {
	if err := os.Mkdir(outputFolder, os.ModePerm); err != nil {
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
		} else if HasSidecarFile(filePath, ".supplemental-metada.json") {
			DuplicateAndFixImage(filePath, outputPath, ".supplemental-metada.json")
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
	meta, err := ReadJsonMetadata(jsonPath)
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
