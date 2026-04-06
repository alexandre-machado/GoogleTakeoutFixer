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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type UnmatchedFile struct {
	MediaPath    string
	Dir          string
	MediaName    string
	JsonsInDir   []string // all .json files in the same directory
	HasPartner   bool     // video has an image partner with sidecar
	DebugInfo    string   // verbose diagnostic info
}

type ScanResult struct {
	TotalMedia     int
	TotalMatched   int
	TotalUnmatched int
	LimitReached   bool
	Limit          int
	Unmatched      []UnmatchedFile
}

// Scan walks a Google Takeout directory and reports all media files
// that have no matching .json sidecar. This helps discover naming
// patterns that FindSidecar doesn't handle yet.
func Scan(sourcePath string, limit int, verbose bool) (*ScanResult, error) {
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("cannot access path: %w", err)
	}
	if !fileInfo.IsDir() {
		return nil, fmt.Errorf("source path is not a directory: %s", sourcePath)
	}

	result := &ScanResult{}

	// Collect directories to scan: the root dir itself + all subdirectories
	type scanDir struct {
		path string
		name string
	}
	dirsToScan := []scanDir{{path: sourcePath, name: filepath.Base(sourcePath)}}

	subdirs, err := DiscoverDirs(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("error discovering directories: %w", err)
	}
	for _, d := range subdirs {
		dirsToScan = append(dirsToScan, scanDir{
			path: filepath.Join(sourcePath, d.Name()),
			name: d.Name(),
		})
	}

scanning:
	for _, dir := range dirsToScan {
		files, err := os.ReadDir(dir.path)
		if err != nil {
			Log(LoggerWarn, "Cannot read directory %s: %v", dir.path, err)
			continue
		}

		// Collect all json files in this directory
		var jsonsInDir []string
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(strings.ToLower(f.Name()), ".json") {
				jsonsInDir = append(jsonsInDir, f.Name())
			}
		}

		for _, f := range files {
			if f.IsDir() {
				continue
			}

			filePath := filepath.Join(dir.path, f.Name())
			if !IsMediaFile(filePath) {
				continue
			}

			result.TotalMedia++

			if limit > 0 && result.TotalMedia > limit {
				result.TotalMedia-- // don't count the one that exceeded
				result.LimitReached = true
				result.Limit = limit
				break scanning
			}

			sidecarPath, err := FindSidecar(filePath)
			if err != nil {
				Log(LoggerWarn, "Error checking sidecar for %s: %v", filePath, err)
				continue
			}

			if sidecarPath != "" {
				result.TotalMatched++
				continue
			}

			// For videos, check if a partner image has a sidecar
			hasPartner := false
			if IsVideoFile(filePath) {
				partner, err := FindImagePartner(filePath)
				if err == nil && partner != "" {
					ps, err := FindSidecar(partner)
					if err == nil && ps != "" {
						hasPartner = true
					}
				}
			}

			if hasPartner {
				result.TotalMatched++
				continue
			}

			// Find candidate .json files that look related to this media file
			candidates := findCandidateJsons(f.Name(), jsonsInDir)

			debugInfo := ""
			if verbose {
				debugInfo = buildDebugInfo(f.Name(), jsonsInDir)
			}

			result.Unmatched = append(result.Unmatched, UnmatchedFile{
				MediaPath:  filePath,
				Dir:        dir.name,
				MediaName:  f.Name(),
				JsonsInDir: candidates,
				HasPartner: false,
				DebugInfo:  debugInfo,
			})
		}
	}

	result.TotalUnmatched = len(result.Unmatched)

	// Sort unmatched by directory then filename
	sort.Slice(result.Unmatched, func(i, j int) bool {
		if result.Unmatched[i].Dir != result.Unmatched[j].Dir {
			return result.Unmatched[i].Dir < result.Unmatched[j].Dir
		}
		return result.Unmatched[i].MediaName < result.Unmatched[j].MediaName
	})

	return result, nil
}

// buildDebugInfo generates byte-level diagnostic info for an unmatched media file
func buildDebugInfo(mediaName string, jsonsInDir []string) string {
	var sb strings.Builder

	mediaLower := strings.ToLower(mediaName)
	base := strings.TrimSuffix(mediaName, filepath.Ext(mediaName))
	ext := strings.ToLower(filepath.Ext(mediaName))

	sb.WriteString(fmt.Sprintf("    [DEBUG] media=%q len=%d bytes\n", mediaName, len(mediaName)))
	sb.WriteString(fmt.Sprintf("    [DEBUG] base=%q ext=%q\n", base, ext))
	sb.WriteString(fmt.Sprintf("    [DEBUG] media hex: %x\n", []byte(mediaName)))

	// Show supplemental candidate that Layer 1 would compute
	const supplementalSuffix = ".supplemental-metadata"
	available := 51 - len(mediaLower) - len(".json")
	sb.WriteString(fmt.Sprintf("    [DEBUG] supplemental available=%d\n", available))
	if available > 0 {
		suffixLen := len(supplementalSuffix)
		if available < suffixLen {
			suffixLen = available
		}
		candidate := mediaLower + supplementalSuffix[:suffixLen] + ".json"
		sb.WriteString(fmt.Sprintf("    [DEBUG] supplemental candidate=%q len=%d\n", candidate, len(candidate)))
		sb.WriteString(fmt.Sprintf("    [DEBUG] candidate hex: %x\n", []byte(candidate)))

		// Check against all jsons
		for _, j := range jsonsInDir {
			jLower := strings.ToLower(j)
			if jLower == candidate {
				sb.WriteString(fmt.Sprintf("    [DEBUG] MATCH FOUND but FindSidecar missed it: %q\n", j))
			}
			// Show hex of closest candidate
			if strings.Contains(strings.ToLower(j), strings.ToLower(base)[:10]) && strings.HasSuffix(jLower, ".json") {
				sb.WriteString(fmt.Sprintf("    [DEBUG] json=%q len=%d hex: %x\n", j, len(j), []byte(j)))
				if len(jLower) == len(candidate) {
					for i := 0; i < len(candidate); i++ {
						if candidate[i] != jLower[i] {
							sb.WriteString(fmt.Sprintf("    [DEBUG] BYTE DIFF at pos %d: candidate=0x%02x json=0x%02x\n", i, candidate[i], jLower[i]))
						}
					}
				} else {
					sb.WriteString(fmt.Sprintf("    [DEBUG] LENGTH DIFF: candidate=%d json=%d\n", len(candidate), len(jLower)))
				}
			}
		}
	}

	return sb.String()
}

// findCandidateJsons returns .json files that look like they could be related
// to the given media file based on shared prefix or substring matching.
func findCandidateJsons(mediaName string, jsonsInDir []string) []string {
	base := strings.TrimSuffix(mediaName, filepath.Ext(mediaName))
	baseLower := strings.ToLower(base)

	// Use a short prefix for fuzzy matching (first 10 chars or whole name)
	shortPrefix := baseLower
	if len(shortPrefix) > 10 {
		shortPrefix = shortPrefix[:10]
	}

	var candidates []string
	for _, j := range jsonsInDir {
		jLower := strings.ToLower(j)
		jsonBase := strings.TrimSuffix(jLower, ".json")

		// Check if json name contains significant part of the media name or vice versa
		if strings.HasPrefix(jLower, shortPrefix) ||
			strings.Contains(jsonBase, baseLower) ||
			strings.Contains(baseLower, strings.TrimSuffix(jsonBase, filepath.Ext(jsonBase))) {
			candidates = append(candidates, j)
		}
	}

	return candidates
}

// FormatScanResult produces a human-readable report of the scan results.
func FormatScanResult(result *ScanResult) string {
	var sb strings.Builder

	sb.WriteString("=== Google Takeout Scan Report ===\n\n")
	if result.LimitReached {
		sb.WriteString(fmt.Sprintf("(scanned first %d media files)\n\n", result.Limit))
	}
	sb.WriteString(fmt.Sprintf("Total media files:   %d\n", result.TotalMedia))
	sb.WriteString(fmt.Sprintf("Matched (sidecar):   %d\n", result.TotalMatched))
	sb.WriteString(fmt.Sprintf("Unmatched:           %d\n", result.TotalUnmatched))

	if result.TotalMedia > 0 {
		pct := float64(result.TotalMatched) / float64(result.TotalMedia) * 100
		sb.WriteString(fmt.Sprintf("Match rate:          %.1f%%\n", pct))
	}

	if result.TotalUnmatched == 0 {
		sb.WriteString("\nAll media files have matching sidecars!\n")
		return sb.String()
	}

	sb.WriteString(fmt.Sprintf("\n--- Unmatched Files (%d) ---\n", result.TotalUnmatched))

	currentDir := ""
	for _, u := range result.Unmatched {
		if u.Dir != currentDir {
			currentDir = u.Dir
			sb.WriteString(fmt.Sprintf("\n[%s]\n", currentDir))
		}

		sb.WriteString(fmt.Sprintf("  %s\n", u.MediaName))
		if len(u.JsonsInDir) > 0 {
			sb.WriteString("    candidate jsons:\n")
			for _, j := range u.JsonsInDir {
				sb.WriteString(fmt.Sprintf("      - %s\n", j))
			}
		} else {
			sb.WriteString("    (no candidate .json files found)\n")
		}
		if u.DebugInfo != "" {
			sb.WriteString(u.DebugInfo)
		}
	}

	// Summary of naming patterns
	sb.WriteString("\n--- Pattern Analysis ---\n")
	patterns := analyzePatterns(result.Unmatched)
	for _, p := range patterns {
		sb.WriteString(fmt.Sprintf("  %s (%d files)\n", p.Description, p.Count))
		if len(p.Examples) > 0 {
			for _, ex := range p.Examples {
				sb.WriteString(fmt.Sprintf("    e.g. %s\n", ex))
			}
		}
	}

	return sb.String()
}

type patternInfo struct {
	Description string
	Count       int
	Examples    []string
}

func analyzePatterns(unmatched []UnmatchedFile) []patternInfo {
	categories := map[string]*patternInfo{
		"no_json":         {Description: "No .json files in directory at all"},
		"has_candidates":  {Description: "Has candidate .json but FindSidecar didn't match"},
		"no_candidates":   {Description: "Has .json files in dir but none look related"},
		"edited":          {Description: "Contains '-edited' in name"},
		"duplicate":       {Description: "Contains '(N)' duplicate marker"},
		"long_name":       {Description: "Filename >= 40 chars (potential truncation issue)"},
		"special_chars":   {Description: "Contains special characters (spaces, unicode, etc.)"},
	}

	for _, u := range unmatched {
		base := strings.TrimSuffix(u.MediaName, filepath.Ext(u.MediaName))

		if len(u.JsonsInDir) == 0 {
			addToPattern(categories["no_json"], u.MediaName)
		} else if len(u.JsonsInDir) > 0 {
			// There are candidate jsons
			addToPattern(categories["has_candidates"], u.MediaName)
		} else {
			addToPattern(categories["no_candidates"], u.MediaName)
		}

		if strings.Contains(strings.ToLower(base), "-edited") {
			addToPattern(categories["edited"], u.MediaName)
		}
		if strings.ContainsAny(base, "()") {
			addToPattern(categories["duplicate"], u.MediaName)
		}
		if len(base) >= 40 {
			addToPattern(categories["long_name"], u.MediaName)
		}
		if strings.ContainsAny(base, " _~#&@!") {
			addToPattern(categories["special_chars"], u.MediaName)
		}
	}

	var result []patternInfo
	for _, p := range categories {
		if p.Count > 0 {
			result = append(result, *p)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Count > result[j].Count
	})

	return result
}

func addToPattern(p *patternInfo, example string) {
	p.Count++
	if len(p.Examples) < 3 {
		p.Examples = append(p.Examples, example)
	}
}
