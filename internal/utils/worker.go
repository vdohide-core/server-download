package utils

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"server-download/internal/logger"
)

// ─── Worker ID ───────────────────────────────────────────────

// GenerateWorkerID generates a unique worker ID.
// Priority: WORKER_ID env → hostname@1
func GenerateWorkerID() string {
	if envWorkerID := os.Getenv("WORKER_ID"); envWorkerID != "" {
		return envWorkerID
	}
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s@1", hostname)
}

// RandomString generates a random alphanumeric string.
// If special=true, inserts dash and underscore (matches randomString from TS).
func RandomString(n int, special bool) string {
	if special {
		return RandomStringSpecial(n)
	}
	return RandomAlphaNum(n)
}

// ─── Process Logger ──────────────────────────────────────────

// ProcessLogger writes to both the global rotating log and a per-process log file.
type ProcessLogger struct {
	file *os.File
}

// NewProcessLogger creates a per-process file logger and redirects global log output
// to io.MultiWriter(globalRotatingLog, processFile) so every log.Printf goes to both.
// On retry, it appends to the existing log file instead of overwriting.
func NewProcessLogger(slug string) *ProcessLogger {
	logDir := filepath.Join("logs", "process")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("⚠️ Failed to create log dir: %v", err)
		return &ProcessLogger{}
	}

	logPath := filepath.Join(logDir, fmt.Sprintf("%s.log", slug))
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("⚠️ Failed to open process log: %v", err)
		return &ProcessLogger{}
	}

	// Redirect global logger → both rotating file + per-process file
	log.SetOutput(io.MultiWriter(logger.GlobalWriter, f))

	return &ProcessLogger{file: f}
}

// Close restores the global logger to the rotating file only and closes the per-process file.
func (pl *ProcessLogger) Close() {
	log.SetOutput(logger.GlobalWriter)
	if pl.file != nil {
		pl.file.Close()
	}
}

// Printf is kept for compatibility but is a no-op — use log.Printf directly.
func (pl *ProcessLogger) Printf(format string, v ...interface{}) {
	log.Printf(format, v...)
}

// ─── Old Log Cleanup ──────────────────────────────────────────

// CleanOldLogs removes process log files older than 7 days.
func CleanOldLogs() {
	logDir := filepath.Join("logs", "process")
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		return
	}

	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(logDir, entry.Name()))
			removed++
		}
	}

	if removed > 0 {
		log.Printf("🧹 Removed %d old log files", removed)
	}
}

// ─── Processing Lock ─────────────────────────────────────────

// ProcessingLock is a mutex-based lock for serializing heavy operations.
type ProcessingLock struct {
	mu   *sync.Mutex
	name string
}

var (
	locksMu sync.Mutex
	locks   = map[string]*sync.Mutex{}
)

// AcquireProcessingLock acquires a named mutex lock (blocking).
// Call Release() when done.
func AcquireProcessingLock(name string) *ProcessingLock {
	locksMu.Lock()
	mu, ok := locks[name]
	if !ok {
		mu = &sync.Mutex{}
		locks[name] = mu
	}
	locksMu.Unlock()

	mu.Lock()
	return &ProcessingLock{mu: mu, name: name}
}

// Release releases the processing lock.
func (l *ProcessingLock) Release() {
	if l.mu != nil {
		l.mu.Unlock()
	}
}
