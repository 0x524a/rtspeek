package rtspeek

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// LogLevel represents different logging levels
type LogLevel int

const (
	LogLevelDisabled LogLevel = iota
	LogLevelError
	LogLevelWarn
	LogLevelInfo
	LogLevelDebug
	LogLevelTrace
)

// Logger wraps zerolog.Logger with RTSP-specific functionality
type Logger struct {
	zlog   zerolog.Logger
	level  LogLevel
	buffer []LogEntry // For compatibility with existing debug trace
}

// LogEntry represents a single log entry for backward compatibility
type LogEntry struct {
	Level     LogLevel               `json:"level"`
	Timestamp time.Time              `json:"timestamp"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// loggerContextKey is used for context-based logger storage
type loggerContextKey struct{}

var loggerKey = loggerContextKey{}

// NewLogger creates a new RTSP logger with the specified configuration
func NewLogger(level LogLevel, output io.Writer, prettyConsole bool) *Logger {
	var zlogger zerolog.Logger

	if prettyConsole && output == os.Stdout {
		// Pretty console output for development
		zlogger = zerolog.New(zerolog.ConsoleWriter{
			Out:        output,
			TimeFormat: time.RFC3339,
			NoColor:    false,
		}).With().Timestamp().Logger()
	} else {
		// JSON output for production/file logging
		zlogger = zerolog.New(output).With().Timestamp().Logger()
	}

	// Set zerolog level
	switch level {
	case LogLevelTrace:
		zlogger = zlogger.Level(zerolog.TraceLevel)
	case LogLevelDebug:
		zlogger = zlogger.Level(zerolog.DebugLevel)
	case LogLevelInfo:
		zlogger = zlogger.Level(zerolog.InfoLevel)
	case LogLevelWarn:
		zlogger = zlogger.Level(zerolog.WarnLevel)
	case LogLevelError:
		zlogger = zlogger.Level(zerolog.ErrorLevel)
	default:
		zlogger = zlogger.Level(zerolog.Disabled)
	}

	return &Logger{
		zlog:   zlogger,
		level:  level,
		buffer: make([]LogEntry, 0),
	}
}

// WithLogger adds a logger to the context
func WithLogger(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// LoggerFromContext extracts logger from context, returns a disabled logger if not found
func LoggerFromContext(ctx context.Context) *Logger {
	if logger, ok := ctx.Value(loggerKey).(*Logger); ok {
		return logger
	}
	// Return disabled logger as fallback
	return NewLogger(LogLevelDisabled, io.Discard, false)
}

// RTSP-specific logging methods

// RTSPRequest logs an outgoing RTSP request
func (l *Logger) RTSPRequest(method, url string, headers map[string][]string) {
	if l.level < LogLevelDebug {
		return
	}

	event := l.zlog.Debug().
		Str("direction", "outgoing").
		Str("type", "request").
		Str("method", method).
		Str("url", url)

	if len(headers) > 0 {
		event = event.Interface("headers", headers)
	}

	event.Msg("RTSP request")

	// Add to buffer for backward compatibility
	l.addToBuffer(LogLevelDebug, "RTSP request", map[string]interface{}{
		"direction": "outgoing",
		"method":    method,
		"url":       url,
		"headers":   headers,
	})
}

// RTSPResponse logs an incoming RTSP response
func (l *Logger) RTSPResponse(statusCode int, statusMessage string, headers map[string][]string) {
	if l.level < LogLevelDebug {
		return
	}

	event := l.zlog.Debug().
		Str("direction", "incoming").
		Str("type", "response").
		Int("status_code", statusCode).
		Str("status_message", statusMessage)

	if len(headers) > 0 {
		event = event.Interface("headers", headers)
	}

	event.Msg("RTSP response")

	// Add to buffer for backward compatibility
	l.addToBuffer(LogLevelDebug, "RTSP response", map[string]interface{}{
		"direction":      "incoming",
		"status_code":    statusCode,
		"status_message": statusMessage,
		"headers":        headers,
	})
}

// Stage logs a processing stage (OPTIONS, DESCRIBE, etc.)
func (l *Logger) Stage(stage string) {
	if l.level < LogLevelDebug {
		return
	}

	l.zlog.Debug().
		Str("stage", stage).
		Msg("Processing stage")

	l.addToBuffer(LogLevelDebug, "Processing stage", map[string]interface{}{
		"stage": stage,
	})
}

// NetworkOperation logs network-level operations
func (l *Logger) NetworkOperation(operation, host string, duration time.Duration, err error) {
	level := LogLevelDebug
	if err != nil {
		level = LogLevelWarn
	}

	if l.level < level {
		return
	}

	event := l.zlog.WithLevel(zerolog.Level(level)).
		Str("operation", operation).
		Str("host", host).
		Dur("duration", duration)

	if err != nil {
		event = event.Err(err)
	}

	event.Msg("Network operation")

	fields := map[string]interface{}{
		"operation": operation,
		"host":      host,
		"duration":  duration,
	}
	if err != nil {
		fields["error"] = err.Error()
	}

	l.addToBuffer(level, "Network operation", fields)
}

// MediaProcessing logs media stream processing
func (l *Logger) MediaProcessing(mediaType string, index int, codec string, resolution string) {
	if l.level < LogLevelInfo {
		return
	}

	l.zlog.Info().
		Str("media_type", mediaType).
		Int("index", index).
		Str("codec", codec).
		Str("resolution", resolution).
		Msg("Media processed")
}

// Error logs errors with context
func (l *Logger) Error(err error, message string, fields ...map[string]interface{}) {
	if l.level < LogLevelError {
		return
	}

	event := l.zlog.Error().Err(err)

	for _, fieldMap := range fields {
		for k, v := range fieldMap {
			event = event.Interface(k, v)
		}
	}

	event.Msg(message)
}

// Info logs informational messages
func (l *Logger) Info(message string, fields ...map[string]interface{}) {
	if l.level < LogLevelInfo {
		return
	}

	event := l.zlog.Info()

	for _, fieldMap := range fields {
		for k, v := range fieldMap {
			event = event.Interface(k, v)
		}
	}

	event.Msg(message)
}

// Debug logs debug messages
func (l *Logger) Debug(message string, fields ...map[string]interface{}) {
	if l.level < LogLevelDebug {
		return
	}

	event := l.zlog.Debug()

	for _, fieldMap := range fields {
		for k, v := range fieldMap {
			event = event.Interface(k, v)
		}
	}

	event.Msg(message)
}

// GetLegacyTrace returns the debug trace in the old string format for backward compatibility
func (l *Logger) GetLegacyTrace() []string {
	if len(l.buffer) == 0 {
		return nil
	}

	trace := make([]string, 0, len(l.buffer))
	for _, entry := range l.buffer {
		// Convert back to old format for compatibility
		if stage, ok := entry.Fields["stage"].(string); ok {
			trace = append(trace, "STAGE: "+stage)
		} else if direction, ok := entry.Fields["direction"].(string); ok {
			if direction == "outgoing" {
				if method, ok := entry.Fields["method"].(string); ok {
					if url, ok := entry.Fields["url"].(string); ok {
						trace = append(trace, "--> "+method+" "+url)
					}
				}
			} else if direction == "incoming" {
				if code, ok := entry.Fields["status_code"].(int); ok {
					if msg, ok := entry.Fields["status_message"].(string); ok {
						trace = append(trace, "← "+string(rune(code))+" "+msg)
					}
				}
			}
		}
	}

	return trace
}

// addToBuffer adds an entry to the internal buffer for backward compatibility
func (l *Logger) addToBuffer(level LogLevel, message string, fields map[string]interface{}) {
	l.buffer = append(l.buffer, LogEntry{
		Level:     level,
		Timestamp: time.Now(),
		Message:   message,
		Fields:    fields,
	})
}

// Clear clears the internal buffer
func (l *Logger) Clear() {
	l.buffer = l.buffer[:0]
}
