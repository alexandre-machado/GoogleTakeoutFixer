package fixer

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// Struct to hold the structure of the JSON metadata
type imageMetadata struct {
	Title          string `json:"title"`
	Description    string `json:"description"`
	PhotoTakenTime struct {
		Timestamp string `json:"timestamp"`
	} `json:"photoTakenTime"`
	GeoData struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Altitude  float64 `json:"altitude"`
	} `json:"geoData"`
}

// Reads JSON and returns some of its metadata contents using the imageMetadata struct
func ReadJsonMetadata(jsonPath string) (imageMetadata, error) {
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

	return data, json.Unmarshal(byteValue, &data)
}

// Helper to find exiftool (bundled or in PATH)
func getExifToolPath() string {
	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		exifName := "exiftool"
		if runtime.GOOS == "windows" {
			exifName = "exiftool.exe"
		}
		bundledPath := filepath.Join(dir, exifName)
		if _, err := os.Stat(bundledPath); err == nil {
			return bundledPath
		}
	}
	return "exiftool"
}

// Apply all available metadata to a file
func ApplyMetadata(filePath string, meta imageMetadata) error {
	timestampInt, err := strconv.ParseInt(meta.PhotoTakenTime.Timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %v", err)
	}

	// TODO: Using .Local() can be false (based on my understanding), fix this
	// newTime := time.Unix(timestampInt, 0).Local()
	newTime := time.Unix(timestampInt, 0).UTC()
	exifTime := newTime.Format("2006:01:02 15:04:05")

	args := []string{
		"-overwrite_original",
		"-AllDates=" + exifTime,
		"-TrackCreateDate=" + exifTime, // Helps with video files i think
		"-MediaCreateDate=" + exifTime,
	}

	// If a title exists, add it to args
	if meta.Title != "" {
		args = append(args, "-Title="+meta.Title)
	}

	// If a description exists, add it to args
	if meta.Description != "" {
		args = append(args, "-ImageDescription="+meta.Description, "-Caption-Abstract="+meta.Description)
	}

	// If geodata exists, add it to args
	// EXIF uses N E S W for geodata
	if meta.GeoData.Latitude != 0 && meta.GeoData.Longitude != 0 {
		lat, lon := meta.GeoData.Latitude, meta.GeoData.Longitude

		latRef, lonRef := "N", "E"
		if lat < 0 {
			latRef = "S"
		}
		if lon < 0 {
			lonRef = "W"
		}

		args = append(args,
			fmt.Sprintf("-GPSLatitude=%f", math.Abs(lat)),
			fmt.Sprintf("-GPSLatitudeRef=%s", latRef),
			fmt.Sprintf("-GPSLongitude=%f", math.Abs(lon)),
			fmt.Sprintf("-GPSLongitudeRef=%s", lonRef),
			fmt.Sprintf("-GPSAltitude=%f", meta.GeoData.Altitude),
			"-CreateDate="+exifTime,
			"-ModifyDate="+exifTime,
		)
	}

	args = append(args, filePath)

	// Run exiftool using the collected args
	// TODO: Starting a new exiftool instance every time is not necessary, reuse instances
	cmd := exec.Command(getExifToolPath(), args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("exiftool error: %v, %s", err, out)
	}

	return nil
}
