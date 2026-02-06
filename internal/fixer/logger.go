package fixer

import "fmt"

type LogLevel string

const (
	LoggerInfo  LogLevel = "INFO"
	LoggerWarn  LogLevel = "WARN"
	LoggerError LogLevel = "ERROR"
)

// LogHandler allows the GUI or CLI to intercept logs.
// If nil, logs are printed to stdout.
var LogHandler func(level LogLevel, message string)

// Log sends a formatted log message to the handler.
func Log(level LogLevel, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if LogHandler != nil {
		LogHandler(level, msg)
	} /*else {
		fmt.Printf("[%s] %s\n", level, msg)
	}*/
}
