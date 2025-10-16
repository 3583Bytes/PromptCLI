package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// Logger handles file-based logging.
type Logger struct {
	enabled bool
	logFile *os.File
}

// NewLogger creates a new logger instance.
func NewLogger() *Logger {
	return &Logger{
		enabled: false,
		logFile: nil,
	}
}

// Setup creates the necessary log directory.
func (l *Logger) Setup() {
	exePath, err := os.Executable()
	if err != nil {
		// For now, we'll just log the error to stderr if we can't find the executable path.
		log.Printf("Error getting executable path for logging: %v", err)
		return
	}
	logDir := filepath.Join(filepath.Dir(exePath), "logs")
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		os.Mkdir(logDir, 0755)
	}
}

// Log writes a message to the log file if logging is enabled.
func (l *Logger) Log(message string) {
	if l.enabled && l.logFile != nil {
		logger := log.New(l.logFile, "", log.LstdFlags)
		logger.Println(message)
	}
}

// Toggle enables or disables logging and returns a status message.
func (l *Logger) Toggle() string {
	l.enabled = !l.enabled
	var logMsg string
	if l.enabled {
		var err error
		exePath, err := os.Executable()
		if err != nil {
			logMsg = fmt.Sprintf("Error getting executable path: %v", err)
		} else {
			logPath := filepath.Join(filepath.Dir(exePath), "logs", "log.txt")
			l.logFile, err = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				logMsg = fmt.Sprintf("Error opening log file: %v", err)
			} else {
				logMsg = "Logging enabled."
			}
		}
	} else {
		if l.logFile != nil {
			l.logFile.Close()
			l.logFile = nil
		}
		logMsg = "Logging disabled."
	}
	return logMsg
}
