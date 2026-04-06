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
	"sync"
	"sync/atomic"
	"time"
)

type LogLevel string

const (
	LoggerInfo  LogLevel = "INFO"
	LoggerWarn  LogLevel = "WARN"
	LoggerError LogLevel = "ERROR"
)

// LogHandler allows the GUI or CLI to intercept logs
var LogHandler func(level LogLevel, message string)

var (
	logFileMu    sync.Mutex
	logFile      *os.File
	logFilePath  string
	logErrCount  int64
	logWarnCount int64
)

// LogDir controls where log files are written. When empty (the default),
// InitializeFileLogger resolves the directory relative to the running
// executable so logs land in a predictable place regardless of the
// working directory at invocation time.
var LogDir string

func InitializeFileLogger() error {
	logFileMu.Lock()
	defer logFileMu.Unlock()

	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}

	// Reset per-run counters.
	atomic.StoreInt64(&logErrCount, 0)
	atomic.StoreInt64(&logWarnCount, 0)

	// Resolve log directory: explicit override → exe dir → cwd fallback.
	dir := LogDir
	if dir == "" {
		if exePath, err := os.Executable(); err == nil {
			dir = filepath.Join(filepath.Dir(exePath), "logs")
		}
	}
	if dir == "" {
		dir = "logs"
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	fileName := fmt.Sprintf("%s.txt", time.Now().Format("2006-01-02_15-04-05"))
	filePath := filepath.Join(dir, fileName)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	logFile = file
	logFilePath = filePath
	return nil
}

// CurrentLogFilePath returns the path of the active log file, or "" if
// file logging has not been initialised.
func CurrentLogFilePath() string {
	logFileMu.Lock()
	defer logFileMu.Unlock()
	return logFilePath
}

func CloseFileLogger() error {
	logFileMu.Lock()
	defer logFileMu.Unlock()

	if logFile == nil {
		return nil
	}

	err := logFile.Close()
	logFile = nil
	return err
}

// Log sends a log message to the file and the registered LogHandler.
// Error and warning counts are tracked atomically so a summary can be
// emitted at the end of a processing run.
func Log(level LogLevel, format string, args ...interface{}) {
	switch level {
	case LoggerError:
		atomic.AddInt64(&logErrCount, 1)
	case LoggerWarn:
		atomic.AddInt64(&logWarnCount, 1)
	}

	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format(time.RFC3339)
	logLine := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, msg)

	logFileMu.Lock()
	if logFile != nil {
		_, _ = logFile.WriteString(logLine)
	}
	logFileMu.Unlock()

	if LogHandler != nil {
		LogHandler(level, msg)
	}
}

// LogCounts returns the number of error and warning messages logged
// since the last call to InitializeFileLogger.
func LogCounts() (errors, warnings int64) {
	return atomic.LoadInt64(&logErrCount), atomic.LoadInt64(&logWarnCount)
}
