package fixer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
	logFileMu sync.Mutex
	logFile   *os.File
)

func InitializeFileLogger() error {
	logFileMu.Lock()
	defer logFileMu.Unlock()

	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}

	if err := os.MkdirAll("logs", 0o755); err != nil {
		return err
	}

	fileName := fmt.Sprintf("%s.txt", time.Now().Format("2006-01-02_15-04-05"))
	filePath := filepath.Join("logs", fileName)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	logFile = file
	return nil
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

// Send a log message to the handler
func Log(level LogLevel, format string, args ...interface{}) {
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
