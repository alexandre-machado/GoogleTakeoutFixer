package fixer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bradfitz/latlong"
)

// Struct to hold the structure of the JSON metadata
type imageMetadata struct {
	Title          string `json:"title"`
	Description    string `json:"description"`
	PhotoTakenTime struct {
		Timestamp string `json:"timestamp"`
		Formatted string `json:"formatted"`
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

// Start a persistent exiftool process
func InitializeExifTool() error {
	exifToolMutex.Lock()
	defer exifToolMutex.Unlock()

	if exifToolCmd != nil {
		// Already initialized
		return nil
	}

	exifToolCmd = exec.Command(getExifToolPath(), "-stay_open", "True", "-@", "-")

	var err error = nil
	exifToolStdin, err = exifToolCmd.StdinPipe()
	if err != nil {
		return err
	}

	exifToolStdout, err = exifToolCmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := exifToolCmd.Start(); err != nil {
		return err
	}

	return nil
}

// Close the persistent exiftool process
func CloseExifTool() {
	exifToolMutex.Lock()
	defer exifToolMutex.Unlock()

	if exifToolCmd != nil {
		exifToolStdin.Write([]byte("-stay_open\nFalse\n"))
		exifToolCmd.Wait()
		exifToolCmd = nil
	}
}

// Apply all available metadata to a file
func ApplyMetadata(filePath string, meta imageMetadata) error {
	timestampInt, err := strconv.ParseInt(meta.PhotoTakenTime.Timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %v", err)
	}

	utcTime := time.Unix(timestampInt, 0).UTC()

	// Determine timezone at the photo's GPS location.
	photoLoc := getPhotoTimezone(meta.GeoData.Latitude, meta.GeoData.Longitude)
	localTime := utcTime.In(photoLoc)
	_, offsetSec := localTime.Zone()
	offsetStr := formatTimezoneOffset(offsetSec)

	exifTime := localTime.Format("2006:01:02 15:04:05")
	// exiftime with timezone
	exifTimeWithTZ := exifTime + offsetStr

	args := []string{
		"-overwrite_original",
		"-AllDates=" + exifTimeWithTZ,
		"-TrackCreateDate=" + exifTimeWithTZ,
		"-MediaCreateDate=" + exifTimeWithTZ,
		"-FileCreateDate=" + exifTimeWithTZ,
		"-FileModifyDate=" + exifTimeWithTZ,
		"-OffsetTime=" + offsetStr,
		"-OffsetTimeOriginal=" + offsetStr,
		"-OffsetTimeDigitized=" + offsetStr,
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
		)
	}

	args = append(args, filePath)

	// Use the persistent exiftool instance
	exifToolMutex.Lock()
	defer exifToolMutex.Unlock()

	if exifToolCmd == nil {
		return fmt.Errorf("Exiftool is not initialized")
	}

	command := strings.Join(args, "\n") + "\n-execute\n"
	if _, err := exifToolStdin.Write([]byte(command)); err != nil {
		return fmt.Errorf("Failed to write to exiftool: %v", err)
	}

	scanner := bufio.NewScanner(exifToolStdout)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Error") {
			return fmt.Errorf("Exiftool error: %s", line)
		}
		if line == "{ready}" {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("Failed to read from exiftool: %v", err)
	}

	// Set the file system modification time to match
	if err := os.Chtimes(filePath, utcTime, utcTime); err != nil {
		return fmt.Errorf("failed to set file timestamps: %v", err)
	}

	return nil
}

// Determine the timezone at a photo's GPS location using the "latlog" library
// If no GPS data is available, fall back to local time
func getPhotoTimezone(lat, lon float64) *time.Location {
	fmt.Println(formatTimezoneOffset(3600))
	if lat == 0 && lon == 0 {
		return time.Local
	}

	tzName := latlong.LookupZoneName(lat, lon)
	fmt.Println(tzName)
	if tzName == "" {
		// Fallback in case latlog fails to find a timezone
		offsetSec := int(math.Round(lon/15.0)) * 3600
		return time.FixedZone("Photo", offsetSec)
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		// Fallback in case loading timezone fails
		offsetSec := int(math.Round(lon/15.0)) * 3600
		return time.FixedZone("Photo", offsetSec)
	}
	return loc
}

// Format a timezone offset in seconds as "+hh:mm" / "-hh:mm" for EXIF
// for example 3600 seconds becomes "+01:00"
func formatTimezoneOffset(offsetSec int) string {
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	hours := offsetSec / 3600
	minutes := (offsetSec % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}

// Exiftool process variables
var (
	exifToolCmd    *exec.Cmd
	exifToolStdin  io.WriteCloser
	exifToolStdout io.ReadCloser
	exifToolMutex  sync.Mutex
)
