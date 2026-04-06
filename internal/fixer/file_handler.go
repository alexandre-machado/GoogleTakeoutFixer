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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Cache for diectory entries to prevent excessive disk reads (issue #5)
var (
	dirCache     = make(map[string][]os.DirEntry)
	dirCacheLock sync.RWMutex
)

// ReadDirCached returns cached directories or reads them it not present
func ReadDirCached(dir string) ([]os.DirEntry, error) {
	dirCacheLock.RLock()
	entries, ok := dirCache[dir]
	dirCacheLock.RUnlock()

	// Cache hit, return entries
	if ok {
		return entries, nil
	}

	dirCacheLock.Lock()
	defer dirCacheLock.Unlock()

	// Check again in case it was created while waiting for lock
	if entries, ok = dirCache[dir]; ok {
		return entries, nil
	}

	// Read directory and cache results
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	dirCache[dir] = entries

	return entries, nil
}

// ClearCache clears the directory cache for all paths
func ClearCache() {
	dirCacheLock.Lock()
	defer dirCacheLock.Unlock()
	// Reallocate map to clear everything
	dirCache = make(map[string][]os.DirEntry)
}

// ClearCacheDir clears the directory cache for a specific path
func ClearCacheDir(dir string) {
	dirCacheLock.Lock()
	defer dirCacheLock.Unlock()
	delete(dirCache, dir)
}

// All media extension to differ between media files and other files
var imageExtensions = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".heic": {},
	".heif": {}, // HEIF container — same format as HEIC, different extension
	".gif":  {},
	".nef":  {}, // Nikon RAW
	".dng":  {}, // Adobe Digital Negative RAW (common in Android exports)
	".webp": {}, // WebP (used by modern Android/Chrome)
	".bmp":  {},
	".tiff": {},
	".tif":  {},
}

var videoExtensions = map[string]struct{}{
	".mp4": {},
	".mov": {},
	".avi": {},
	".mkv": {},
	".3gp": {}, // 3GPP — old Android video format
	".m4v": {}, // Apple video container (essentially MP4)
	".wmv": {},
	".flv": {},
}

// Checks whether a file is a video file based on its extension
func IsVideoFile(path string) bool {
	extension := filepath.Ext(path)
	_, ok := videoExtensions[strings.ToLower(extension)]
	return ok
}

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

const supplementalSuffix = ".supplemental-metadata"

// isSupplementalMatch checks if a lowercased json filename matches the pattern
// "imageNameLower + <truncated .supplemental-metadata> + .json".
// Uses prefix+suffix matching instead of exact byte-length computation to handle
// double-encoded UTF-8 filenames (e.g. from OneDrive sync).
func isSupplementalMatch(jsonLower string, imageNameLower string) bool {
	if !strings.HasSuffix(jsonLower, ".json") {
		return false
	}
	jsonBase := strings.TrimSuffix(jsonLower, ".json")
	if !strings.HasPrefix(jsonBase, imageNameLower) {
		return false
	}
	remainder := jsonBase[len(imageNameLower):]
	// remainder should be a prefix of ".supplemental-metadata" (possibly truncated)
	return len(remainder) > 0 && strings.HasPrefix(supplementalSuffix, remainder)
}

// Find a matching sidecar JSON
func FindSidecar(imagePath string) (string, error) {
	dir := filepath.Dir(imagePath)
	imageName := filepath.Base(imagePath)
	base := strings.TrimSuffix(imageName, filepath.Ext(imageName))
	prefix := strings.ToLower(base)
	ext := strings.ToLower(filepath.Ext(imageName))

	entries, err := ReadDirCached(dir)
	if err != nil {
		return "", err
	}

	imageNameLower := strings.ToLower(imageName)

	// 1. Try exact matches first
	for _, entry := range entries {
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".json") {
			continue
		}

		// Standard: image.jpg.json
		if lower == imageNameLower+".json" {
			return filepath.Join(dir, name), nil
		}

		// Alternative: image.json
		if strings.TrimSuffix(lower, ".json") == prefix {
			return filepath.Join(dir, name), nil
		}

		// Supplemental metadata: Google Takeout creates sidecars like
		// "image.jpg.supplemental-metadata.json", truncated to ~51 chars.
		// Instead of computing exact byte length (which breaks with double-encoded UTF-8),
		// use prefix+suffix matching: json starts with imageName and the middle part
		// is a prefix of ".supplemental-metadata".
		// Two variants: "image.jpg.supplemental-metadata.json" and "image.supplemental-metadata.json"
		if isSupplementalMatch(lower, imageNameLower) || isSupplementalMatch(lower, prefix) {
			return filepath.Join(dir, name), nil
		}
	}

	// 2. Fallback for Google Photos duplicates (e.g. "image(1).jpg" -> "image.jpg.supplemental-metadata(1).json")
	duplicateRegex := regexp.MustCompile(`^(.*)\((\d+)\)$`)
	if match := duplicateRegex.FindStringSubmatch(prefix); match != nil {
		realBase := match[1]
		num := match[2]

		expectedJson1 := fmt.Sprintf("%s%s.supplemental-metadata(%s).json", realBase, ext, num)
		expectedJson2 := fmt.Sprintf("%s%s(%s).json", realBase, ext, num)
		expectedJson3 := fmt.Sprintf("%s(%s).json", realBase, num)
		expectedJson4 := fmt.Sprintf("%s.supplemental-metadata(%s).json", realBase, num)

		// Also build the original image name (without duplicate number) for matching
		realImageName := realBase + ext
		numSuffix := fmt.Sprintf("(%s)", num)

		for _, entry := range entries {
			name := entry.Name()
			lower := strings.ToLower(name)
			if lower == expectedJson1 || lower == expectedJson2 || lower == expectedJson3 || lower == expectedJson4 {
				return filepath.Join(dir, name), nil
			}
			// Truncated supplemental for the duplicate filename itself
			// e.g. "image(2).jpg" -> "image(2).jpg..json"
			if isSupplementalMatch(lower, imageNameLower) {
				return filepath.Join(dir, name), nil
			}
			// Truncated supplemental with (N) after the truncated suffix
			// e.g. "image(1).jpg" -> "image.jpg.supplemental-metad(1).json"
			if strings.HasSuffix(lower, ".json") {
				jsonBase := strings.TrimSuffix(lower, ".json")
				for _, baseVariant := range []string{realImageName, realBase} {
					prefix := baseVariant + "."
					if strings.HasPrefix(jsonBase, prefix) && strings.HasSuffix(jsonBase, numSuffix) {
						middle := jsonBase[len(baseVariant) : len(jsonBase)-len(numSuffix)]
						if strings.HasPrefix(supplementalSuffix, middle) {
							return filepath.Join(dir, name), nil
						}
					}
				}
			}
		}
	}

	// 3. Fallback for truncated filenames (Google Takeout truncates sidecar names to 51 chars)
	// Two truncation modes:
	//   a) The image base name (without ext) is truncated in the JSON filename
	//   b) The full image name (with ext) is truncated, cutting into the extension
	//      e.g. "long name.jpg" -> sidecar "long name.j.json" (51 chars total)
	if len(prefix) >= 40 || len(imageNameLower) > 46 {
		for _, entry := range entries {
			name := entry.Name()
			lower := strings.ToLower(name)
			if !strings.HasSuffix(lower, ".json") {
				continue
			}
			jsonBase := strings.TrimSuffix(lower, ".json")
			if len(jsonBase) < 40 {
				continue
			}
			// (a) JSON base is a truncated version of the image base (without ext)
			if strings.HasPrefix(prefix, jsonBase) {
				return filepath.Join(dir, name), nil
			}
			// (b) JSON base is a truncated version of the full image name (with ext)
			if strings.HasPrefix(imageNameLower, jsonBase) {
				return filepath.Join(dir, name), nil
			}
		}
	}

	// 4. Fallback for Google Photos "-edited" files (including "-edited(N)" combos)
	// e.g. "IMG-edited.jpg" -> "IMG.jpg.json" or "IMG.supplemental-metadata.json"
	// e.g. "IMG-edited(1).jpg" -> "IMG.jpg.supplemental-metadata(1).json"
	editedRegex := regexp.MustCompile(`^(.*)-edited(?:\((\d+)\))?$`)
	if editedMatch := editedRegex.FindStringSubmatch(prefix); editedMatch != nil {
		prefixOriginal := editedMatch[1]
		editedNum := editedMatch[2] // may be empty
		originalImageName := prefixOriginal + ext

		for _, entry := range entries {
			name := entry.Name()
			lower := strings.ToLower(name)
			if !strings.HasSuffix(lower, ".json") {
				continue
			}
			jsonBase := strings.TrimSuffix(lower, ".json")

			// original.json
			if jsonBase == prefixOriginal {
				return filepath.Join(dir, name), nil
			}

			// original.jpg.json
			if jsonBase == originalImageName {
				return filepath.Join(dir, name), nil
			}

			// original.jpg.supplemental-metadata.json or original.supplemental-metadata.json
			if isSupplementalMatch(lower, originalImageName) || isSupplementalMatch(lower, prefixOriginal) {
				return filepath.Join(dir, name), nil
			}

			// For "-edited(N)": look for "original.jpg.supplemental-metadata(N).json" patterns
			if editedNum != "" {
				expected1 := fmt.Sprintf("%s.supplemental-metadata(%s).json", originalImageName, editedNum)
				expected2 := fmt.Sprintf("%s(%s).json", originalImageName, editedNum)
				expected3 := fmt.Sprintf("%s.supplemental-metadata(%s).json", prefixOriginal, editedNum)
				expected4 := fmt.Sprintf("%s(%s).json", prefixOriginal, editedNum)
				if lower == expected1 || lower == expected2 || lower == expected3 || lower == expected4 {
					return filepath.Join(dir, name), nil
				}
			}

			// Truncated long filenames fallback
			if len(jsonBase) >= 40 {
				if strings.HasPrefix(prefixOriginal, jsonBase) || strings.HasPrefix(originalImageName, jsonBase) {
					return filepath.Join(dir, name), nil
				}
			}
		}
	}

	// 5. Fallback for truncated media filenames with "-edited" or "(N)-edited"
	// e.g. "longname(2)-edite.jpg" is a truncated "longname(2)-edited.jpg"
	// Try matching against the sidecar for "longname(2).jpg"
	if strings.Contains(prefix, "-e") && len(prefix) >= 40 {
		// Try stripping various truncations of "-edited" (from most complete to least)
		for _, suffix := range []string{"-edited", "-edite", "-edit", "-edi", "-ed", "-e"} {
			if !strings.HasSuffix(prefix, suffix) {
				continue
			}
			strippedBase := strings.TrimSuffix(prefix, suffix)
			strippedImageName := strippedBase + ext
			for _, entry := range entries {
				name := entry.Name()
				lower := strings.ToLower(name)
				if !strings.HasSuffix(lower, ".json") {
					continue
				}
				jsonBase := strings.TrimSuffix(lower, ".json")
				if jsonBase == strippedBase || jsonBase == strippedImageName {
					return filepath.Join(dir, name), nil
				}
				if isSupplementalMatch(lower, strippedImageName) || isSupplementalMatch(lower, strippedBase) {
					return filepath.Join(dir, name), nil
				}
				// Truncated sidecar: jsonBase is a prefix of the stripped image name
				if len(jsonBase) >= 40 {
					if strings.HasPrefix(strippedBase, jsonBase) || strings.HasPrefix(strippedImageName, jsonBase) {
						return filepath.Join(dir, name), nil
					}
				}
			}
			break
		}
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
	// yearPrefixes is mostly made by AI. I have not verified these, but i assume they are primarily correct.
	// Please create an issue if you find any mistakes or if you want to add more languages.
	yearPrefixes := []string{
		"Photos from ",     // English
		"Fotos von ",       // German
		"Photos de ",       // French
		"Foto del ",        // Italian
		"Fotos de ",        // Spanish / Portuguese
		"Foto's van ",      // Dutch
		"Zdjęcia z ",       // Polish
		"Фотографии из ",   // Russian
		"Foton från ",      // Swedish
		"Bilder fra ",      // Norwegian
		"Billeder fra ",    // Danish
		"Fotoğraflar ",     // Turkish
		"Fotografie z ",    // Czech
		"Fotók a ",         // Hungarian
		"Φωτογραφίες από ", // Greek
		"Fotografii din ",  // Romanian
		"Foto dari ",       // Indonesian
		"รูปภาพจาก ",       // Thai
		"Ảnh từ ",          // Vietnamese
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
	if isImage || isVideo {
		return true
	}
	// No extension: probe the first few bytes to detect common media formats.
	// Google Photos sometimes exports files without an extension when the
	// original upload had none. We check magic bytes to avoid false positives.
	if extension == "" {
		return sniffMediaMagic(path)
	}
	return false
}

// detectExtensionByMagic reads up to 12 bytes from path and returns the
// appropriate file extension (e.g. ".jpg") for known image/video formats,
// or "" if the format is unrecognised. Used only for extensionless files.
func detectExtensionByMagic(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 12)
	n, err := f.Read(buf)
	if err != nil || n < 4 {
		return ""
	}

	// JPEG: FF D8 FF
	if buf[0] == 0xFF && buf[1] == 0xD8 && buf[2] == 0xFF {
		return ".jpg"
	}
	// PNG: 89 50 4E 47
	if buf[0] == 0x89 && buf[1] == 0x50 && buf[2] == 0x4E && buf[3] == 0x47 {
		return ".png"
	}
	// GIF: GIF8
	if buf[0] == 0x47 && buf[1] == 0x49 && buf[2] == 0x46 && buf[3] == 0x38 {
		return ".gif"
	}
	// WebP: RIFF????WEBP
	if n >= 12 && buf[0] == 0x52 && buf[1] == 0x49 && buf[2] == 0x46 && buf[3] == 0x46 &&
		buf[8] == 0x57 && buf[9] == 0x45 && buf[10] == 0x42 && buf[11] == 0x50 {
		return ".webp"
	}
	// HEIC/HEIF and ISO Base Media (MP4, MOV, 3GP, M4V): ftyp box at offset 4
	if n >= 8 && buf[4] == 0x66 && buf[5] == 0x74 && buf[6] == 0x79 && buf[7] == 0x70 {
		// Distinguish HEIC/HEIF from video containers by the brand code at offset 8
		if n >= 12 {
			brand := string(buf[8:12])
			switch brand {
			case "heic", "heix", "hevc", "hevx", "heim", "heis", "hevm", "hevs", "mif1", "msf1":
				return ".heic"
			}
		}
		return ".mp4"
	}

	return ""
}

// sniffMediaMagic returns true when the file at path begins with the magic
// bytes of a known image or video format. Used by IsMediaFile for extensionless files.
func sniffMediaMagic(path string) bool {
	return detectExtensionByMagic(path) != ""
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

// Detect the month of a file based on its sidecar metadata
// Returns the month as an integer between 1 and 12.
// If the sidecar is missing or is a cloud-sync placeholder
// (ErrSidecarOffline), it falls back to the source file's mtime so that
// --month-subfolders continues to work during batched OneDrive migrations.
func DetectFileMonth(sourcePath string, sidecarPath string) (int, error) {
	if sidecarPath != "" {
		metadata, err := ReadJsonMetadata(sidecarPath)
		if err == nil {
			timestamp, tErr := strconv.ParseInt(metadata.PhotoTakenTime.Timestamp, 10, 64)
			if tErr == nil {
				return int(time.Unix(timestamp, 0).Month()), nil
			}
		} else if !errors.Is(err, ErrSidecarOffline) {
			return 0, err
		}
		// Offline placeholder: fall through to mtime fallback below.
	}

	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return 0, err
	}

	return int(fileInfo.ModTime().Month()), nil
}

func ResolveOutputDir(
	fixerCtx *FixerContext,
	sourcePath string,
	sidecarPath string,
	sourceDirName string,
	isYearFolder bool,
) (string, error) {
	if fixerCtx.Options.Flatten {
		return fixerCtx.OutputRoot, nil
	}

	targetDir := fixerCtx.OutputRoot
	if sourceDirName != "" /*&& !fixerCtx.Options.IgnoreAlbums && !isYearFolder*/ {
		targetDir = filepath.Join(targetDir, sourceDirName)
	}

	if !fixerCtx.Options.MonthSubfolders {
		return targetDir, nil
	}

	month, err := DetectFileMonth(sourcePath, sidecarPath)
	if err != nil {
		return "", err
	}

	return filepath.Join(targetDir, strconv.Itoa(month)), nil
}
