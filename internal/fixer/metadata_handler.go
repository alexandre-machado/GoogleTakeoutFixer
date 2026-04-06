/*
GoogleTakeoutFixer - A tool to easily clean and organize Google Photos Takeout exports
Copyright (C) 2026 feloex

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package fixer

import (
	"bufio"
	"encoding/json"
	"errors"
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
	_ "time/tzdata"

	"github.com/bradfitz/latlong"
)

// ErrSidecarOffline is returned when a sidecar JSON appears to be a
// cloud-sync placeholder (e.g., OneDrive Files On-Demand) that has not
// been downloaded locally. Callers should treat this as a hydration
// problem rather than a corrupt JSON and surface a clear instruction
// to the user instead of a generic parse error.
var ErrSidecarOffline = errors.New("sidecar is a cloud placeholder (not downloaded locally)")

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
	GeoDataExif struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Altitude  float64 `json:"altitude"`
	} `json:"geoDataExif"`
}

// Reads JSON and returns some of its metadata contents using the imageMetadata struct.
// Returns ErrSidecarOffline if the sidecar is detected as a cloud-sync placeholder
// rather than a genuine JSON parse error.
func ReadJsonMetadata(jsonPath string) (imageMetadata, error) {
	var data imageMetadata

	// Cheap pre-check via os.Stat: on Windows this detects NTFS Cloud Files
	// placeholders (OneDrive / iCloud) without triggering hydration, and the
	// zero-size heuristic works cross-platform.
	if info, err := os.Stat(jsonPath); err == nil {
		if isCloudPlaceholder(info) || info.Size() == 0 {
			return data, ErrSidecarOffline
		}
	}

	jsonFile, err := os.Open(jsonPath)
	if err != nil {
		return data, err
	}
	defer jsonFile.Close()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return data, err
	}

	// Final defense: an otherwise-valid-looking file whose first byte is NUL
	// is almost certainly a zero-padded cloud placeholder that slipped past
	// the stat check (some sync clients report a fake size). Any real JSON
	// starts with '{', '[', whitespace, or a BOM — never NUL.
	if len(byteValue) == 0 || byteValue[0] == 0x00 {
		return data, ErrSidecarOffline
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
	exifToolScanner = bufio.NewScanner(exifToolStdout)

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
	lat := meta.GeoData.Latitude
	lon := meta.GeoData.Longitude
	alt := meta.GeoData.Altitude

	// Fallback to GeoDataExif if GeoData is empty (Google Takeout quirk)
	if lat == 0 && lon == 0 {
		lat = meta.GeoDataExif.Latitude
		lon = meta.GeoDataExif.Longitude
		alt = meta.GeoDataExif.Altitude
	}

	photoLoc := getPhotoTimezone(lat, lon)
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
	if lat != 0 && lon != 0 {
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
			fmt.Sprintf("-GPSAltitude=%f", alt),
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

	var exifErr error
	for exifToolScanner.Scan() {
		line := exifToolScanner.Text()
		if line == "{ready}" {
			break
		}
		if strings.Contains(line, "Error") && exifErr == nil {
			exifErr = fmt.Errorf("Exiftool error: %s", line)
		}
	}

	if exifErr != nil {
		return exifErr
	}

	if err := exifToolScanner.Err(); err != nil {
		return fmt.Errorf("Failed to read from exiftool: %v", err)
	}

	// Set the file system modification time to match
	if err := os.Chtimes(filePath, utcTime, utcTime); err != nil {
		return fmt.Errorf("failed to set file timestamps: %v", err)
	}

	return nil
}

// GetMajorBrand reads the MajorBrand tag from a file using the persistent exiftool instance
func GetMajorBrand(filePath string) (string, error) {
	exifToolMutex.Lock()
	defer exifToolMutex.Unlock()

	if exifToolCmd == nil {
		return "", fmt.Errorf("exiftool not initialized")
	}

	if _, err := fmt.Fprintf(exifToolStdin, "-MajorBrand\n-s3\n-charset\nfilename=utf8\n%s\n-execute\n", filePath); err != nil {
		return "", err
	}

	var majorBrand string
	for exifToolScanner.Scan() {
		if line := exifToolScanner.Text(); line == "{ready}" {
			break
		} else if majorBrand == "" && !strings.Contains(line, "Error") {
			majorBrand = line
		}
	}

	return strings.TrimSpace(majorBrand), exifToolScanner.Err()
}

// Determine the timezone at a photo's GPS location using the "latlog" library
// If no GPS data is available, fall back to local time
func getPhotoTimezone(lat, lon float64) *time.Location {
	if lat == 0 && lon == 0 {
		return time.Local
	}

	tzName := latlong.LookupZoneName(lat, lon)
	if tzName == "" {
		// Fallback in case latlog fails to find a timezone
		Log(LoggerWarn, "Could not look up timezone for coordinates lat=%f, lon=%f", lat, lon)
		offsetSec := int(math.Round(lon/15.0)) * 3600
		return time.FixedZone("Photo", offsetSec)
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		// Fallback in case loading timezone fails
		Log(LoggerWarn, "Could not load timezone '%s'", tzName)
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
	exifToolCmd     *exec.Cmd
	exifToolStdin   io.WriteCloser
	exifToolStdout  io.ReadCloser
	exifToolScanner *bufio.Scanner
	exifToolMutex   sync.Mutex
)
