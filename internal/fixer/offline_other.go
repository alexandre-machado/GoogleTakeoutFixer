//go:build !windows

/*
GoogleTakeoutFixer - A tool to easily clean and organize Google Photos Takeout exports
Copyright (C) 2026 feloex

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/

package fixer

import "os"

// isCloudPlaceholder reports whether info describes a cloud-sync
// placeholder. Cloud-file attributes are OS-specific: on non-Windows
// platforms this is a stub that always returns false, and callers
// fall back to the zero-size heuristic.
func isCloudPlaceholder(info os.FileInfo) bool {
	return false
}
