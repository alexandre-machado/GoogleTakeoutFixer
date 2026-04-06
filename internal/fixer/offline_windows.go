//go:build windows

/*
GoogleTakeoutFixer - A tool to easily clean and organize Google Photos Takeout exports
Copyright (C) 2026 feloex

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/

package fixer

import (
	"os"
	"syscall"
)

// NTFS Cloud Files attributes used by OneDrive Files On-Demand,
// iCloud, and similar cloud-sync providers. A file with any of these
// attributes set is a placeholder whose contents are not available
// locally without triggering a network download.
//
// syscall exposes FILE_ATTRIBUTE_OFFLINE but not the Cloud Files API
// attributes, so we define them here.
const (
	fileAttributeOffline            = 0x1000
	fileAttributeRecallOnOpen       = 0x40000
	fileAttributeRecallOnDataAccess = 0x400000
)

// isCloudPlaceholder reports whether info describes a file that is a
// cloud-sync placeholder not fully present on the local disk. Callers
// should treat a true result as "skip and instruct the user to hydrate".
//
// Important: checking these attributes via os.Stat does NOT trigger a
// download — only reading the file content does.
func isCloudPlaceholder(info os.FileInfo) bool {
	attrs, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false
	}
	const mask = fileAttributeOffline | fileAttributeRecallOnOpen | fileAttributeRecallOnDataAccess
	return attrs.FileAttributes&mask != 0
}
