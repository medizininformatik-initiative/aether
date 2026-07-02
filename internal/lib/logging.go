package lib

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"
)

// LogLevel defines the severity of log messages
type LogLevel int

const (
	LogLevelDebug LogLevel = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
)

// Logger provides structured logging for the application.
//
// It fans out to two independent sinks: a console sink filtered by the
// configured level, and an optional per-job file sink that always records full
// DEBUG output so jobs/<job-id>/job.log is a complete diagnostic record
// regardless of the console verbosity.
type Logger struct {
	level      LogLevel
	console    *log.Logger
	fileLogger *log.Logger
	file       *os.File
}

// NewLogger creates a new logger that writes console output to stderr.
func NewLogger(level LogLevel) *Logger {
	return NewLoggerWithWriter(level, os.Stderr)
}

// NewLoggerWithWriter creates a logger whose console sink writes to w. Primarily
// used for tests that need to capture and assert on console output.
func NewLoggerWithWriter(level LogLevel, w io.Writer) *Logger {
	return &Logger{
		level:   level,
		console: log.New(w, "", log.LstdFlags),
	}
}

// AttachJobLogFile routes a full-DEBUG copy of every log line to the file at
// path, opened in append mode so repeated runs (e.g. pipeline continue) extend
// one chronological record. Call Close to release the handle.
func (l *Logger) AttachJobLogFile(path string) error {
	// Close any previously attached file so re-attaching never leaks a handle.
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
		l.fileLogger = nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening job log file %s: %w", path, err)
	}
	l.file = f
	l.fileLogger = log.New(f, "", log.LstdFlags)
	return nil
}

// Close releases the attached job log file, if any. Safe to call when no file
// is attached.
func (l *Logger) Close() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	l.fileLogger = nil
	return err
}

// DefaultLogger returns a logger with INFO level
var DefaultLogger = NewLogger(LogLevelInfo)

// Debug logs a debug message
func (l *Logger) Debug(message string, fields ...any) {
	l.log(LogLevelDebug, "DEBUG", message, fields...)
}

// Info logs an informational message
func (l *Logger) Info(message string, fields ...any) {
	l.log(LogLevelInfo, "INFO", message, fields...)
}

// Warn logs a warning message
func (l *Logger) Warn(message string, fields ...any) {
	l.log(LogLevelWarn, "WARN", message, fields...)
}

// Error logs an error message
func (l *Logger) Error(message string, fields ...any) {
	l.log(LogLevelError, "ERROR", message, fields...)
}

// formatLogLine renders a single structured log line shared by every sink.
func formatLogLine(label string, message string, fields ...any) string {
	var fieldsStr string
	if len(fields) > 0 {
		fieldsStr = fmt.Sprintf(" | %v", fields)
	}
	return fmt.Sprintf("[%s] %s%s", label, message, fieldsStr)
}

// log formats a message once and fans it out: to the console only when its
// severity meets the configured level, and to the attached job log file always.
func (l *Logger) log(lvl LogLevel, label string, message string, fields ...any) {
	line := formatLogLine(label, message, fields...)

	if lvl >= l.level {
		l.console.Println(line)
	}
	if l.fileLogger != nil {
		l.fileLogger.Println(line)
	}
}

// logFailure records a structured failure line. When a job log file is attached
// it writes there only, keeping the console clean because the failure is already
// surfaced to the user as a top-level `Error:` line. With no file attached it
// falls back to the console so the record is never silently dropped.
func (l *Logger) logFailure(label string, message string, fields ...any) {
	line := formatLogLine(label, message, fields...)
	if l.fileLogger != nil {
		l.fileLogger.Println(line)
		return
	}
	l.console.Println(line)
}

// LogOperation logs the start and completion of an operation
func LogOperation(logger *Logger, operation string, fn func() error) error {
	logger.Info(fmt.Sprintf("Starting: %s", operation))
	start := time.Now()

	err := fn()

	duration := time.Since(start)
	if err != nil {
		logger.Error(fmt.Sprintf("Failed: %s", operation), "duration", duration, "error", err)
		return err
	}

	logger.Info(fmt.Sprintf("Completed: %s", operation), "duration", duration)
	return nil
}

// LogRetry logs retry attempts
func LogRetry(logger *Logger, operation string, attempt int, maxAttempts int, err error) {
	// Remove line breaks from operation to prevent log spoofing
	safeOperation := strings.ReplaceAll(operation, "\n", "")
	safeOperation = strings.ReplaceAll(safeOperation, "\r", "")
	logger.Warn(
		fmt.Sprintf("Retry attempt %d/%d for: %s", attempt+1, maxAttempts, safeOperation),
		"error", err,
	)
}

// LogStepStart logs the start of a pipeline step
func LogStepStart(logger *Logger, stepName string, jobID string) {
	logger.Info(
		"Step started",
		"step", stepName,
		"job_id", jobID,
	)
}

// LogStepComplete logs the completion of a pipeline step
func LogStepComplete(logger *Logger, stepName string, jobID string, filesProcessed int, duration time.Duration) {
	logger.Info(
		"Step completed",
		"step", stepName,
		"job_id", jobID,
		"files", filesProcessed,
		"duration", duration,
	)
}

// LogStepFailed records a failed pipeline step to the job log file only. The
// failure is surfaced to the user as a top-level `Error:` line, so logging it to
// the console as well would duplicate the same error on stderr.
func LogStepFailed(logger *Logger, stepName string, jobID string, err error, retryable bool) {
	logger.logFailure(
		"ERROR",
		"Step failed",
		"step", stepName,
		"job_id", jobID,
		"error", err,
		"retryable", retryable,
	)
}

// LogJobCreated logs job creation
func LogJobCreated(logger *Logger, jobID string, inputSource string) {
	logger.Info(
		"Job created",
		"job_id", jobID,
		"input_source", inputSource,
	)
}

// LogJobCompleted logs job completion
func LogJobCompleted(logger *Logger, jobID string, totalFiles int, duration time.Duration) {
	logger.Info(
		"Job completed",
		"job_id", jobID,
		"total_files", totalFiles,
		"duration", duration,
	)
}

// LogServiceCall logs HTTP service calls
func LogServiceCall(logger *Logger, service string, endpoint string, method string) {
	logger.Debug(
		"Service call",
		"service", service,
		"endpoint", endpoint,
		"method", method,
	)
}

// LogServiceResponse logs HTTP service responses
func LogServiceResponse(logger *Logger, service string, statusCode int, duration time.Duration) {
	if statusCode >= 400 {
		logger.Warn(
			"Service response",
			"service", service,
			"status", statusCode,
			"duration", duration,
		)
	} else {
		logger.Debug(
			"Service response",
			"service", service,
			"status", statusCode,
			"duration", duration,
		)
	}
}

// SetLevel changes the log level
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// ParseLogLevel converts a string to LogLevel
func ParseLogLevel(levelStr string) LogLevel {
	switch levelStr {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn":
		return LogLevelWarn
	case "error":
		return LogLevelError
	default:
		return LogLevelInfo
	}
}
