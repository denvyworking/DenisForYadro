package logger

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

type Logger struct {
	mu    sync.Mutex
	std   *log.Logger
	level Level
}

type Field map[string]any

func New(level string) *Logger {
	return &Logger{
		std:   log.New(os.Stdout, "", 0),
		level: parseLevel(level),
	}
}

func parseLevel(level string) Level {
	switch strings.ToLower(level) {
	case string(LevelDebug):
		return LevelDebug
	case string(LevelWarn):
		return LevelWarn
	case string(LevelError):
		return LevelError
	default:
		return LevelInfo
	}
}

func (l *Logger) Enabled(level Level) bool {
	switch l.level {
	case LevelDebug:
		return true
	case LevelInfo:
		return level != LevelDebug
	case LevelWarn:
		return level == LevelWarn || level == LevelError
	case LevelError:
		return level == LevelError
	default:
		return true
	}
}

func (l *Logger) log(level Level, msg string, fields Field) {
	if !l.Enabled(level) {
		return
	}

	entry := map[string]any{
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
		"level": string(level),
		"msg":   msg,
	}
	for key, value := range fields {
		entry[key] = value
	}

	data, err := json.Marshal(entry)
	if err != nil {
		l.std.Printf("{\"ts\":\"%s\",\"level\":\"error\",\"msg\":\"logger marshal failed\"}", time.Now().UTC().Format(time.RFC3339Nano))
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.std.Writer().Write(append(data, '\n'))
}

func (l *Logger) Debug(msg string, fields Field) { l.log(LevelDebug, msg, fields) }
func (l *Logger) Info(msg string, fields Field)  { l.log(LevelInfo, msg, fields) }
func (l *Logger) Warn(msg string, fields Field)  { l.log(LevelWarn, msg, fields) }
func (l *Logger) Error(msg string, fields Field) { l.log(LevelError, msg, fields) }

func (l *Logger) WithRequest(r *Request) Field {
	return Field{
		"request_id":     r.ID,
		"method":         r.Method,
		"path":           r.Path,
		"remote_addr":    r.RemoteAddr,
		"status_code":    r.Status,
		"duration_ms":    r.Duration.Milliseconds(),
		"content_type":   r.ContentType,
		"content_length": r.ContentLength,
	}
}

type Request struct {
	ID            string
	Method        string
	Path          string
	RemoteAddr    string
	Status        int
	Duration      time.Duration
	ContentType   string
	ContentLength int64
}

func NopWriter() io.Writer {
	return io.Discard
}
