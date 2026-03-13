// Package log provides unified logging via zap. Use log.Info(), log.Debugf(), etc.
// Call log.Init(level, writer) once at startup to set level and output (e.g. file).
// Package log provides unified logging for the whole project. All levels (debug, info, warn, error)
// are written to the same output. Call log.InitWithFile(level, logFile) once at application startup
// (e.g. from main after loading config); do not configure log in individual packages (e.g. TUI).
package log

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	sugared *zap.SugaredLogger
)

func init() {
	// Default: stderr, info level, until Init or InitWithFile is called
	Init("info", os.Stderr)
}

// InitWithFile configures the global logger for the whole project. level is "debug", "info", "warn", or "error".
// logFile is the path for log output; all levels are written to this file. If empty, logs go to os.Stderr.
// "~" at the start of logFile is expanded to the user's home directory.
// Call this once at startup (e.g. in main after loading config).
func InitWithFile(level string, logFile string) {
	var w io.Writer = os.Stderr
	if logFile != "" {
		logFile = expandHomePath(logFile)
		if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err == nil {
			if f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
				w = f
			}
		}
	}
	Init(level, w)
}

// expandHomePath replaces leading "~" with the user's home directory.
func expandHomePath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	if len(path) > 1 && path[1] != '/' && path[1] != os.PathSeparator {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

// Init configures the global logger. level is "debug", "info", "warn", or "error".
// w is the output; if nil, os.Stderr is used.
func Init(level string, w io.Writer) {
	if w == nil {
		w = os.Stderr
	}
	enc := zapcore.NewConsoleEncoder(zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		MessageKey:     "msg",
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	})
	core := zapcore.NewCore(enc, zapcore.AddSync(w), parseLevel(level))
	logger := zap.New(core, zap.AddCallerSkip(1))
	sugared = logger.Sugar()
}

func parseLevel(s string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// Debug logs at Debug level. Arguments are handled like fmt.Sprint.
func Debug(args ...any) {
	sugared.Debug(args...)
}

// Debugf logs at Debug level. Arguments are handled like fmt.Sprintf.
func Debugf(format string, args ...any) {
	sugared.Debugf(format, args...)
}

// Info logs at Info level.
func Info(args ...any) {
	sugared.Info(args...)
}

// Infof logs at Info level with format.
func Infof(format string, args ...any) {
	sugared.Infof(format, args...)
}

// Warn logs at Warn level.
func Warn(args ...any) {
	sugared.Warn(args...)
}

// Warnf logs at Warn level with format.
func Warnf(format string, args ...any) {
	sugared.Warnf(format, args...)
}

// Error logs at Error level.
func Error(args ...any) {
	sugared.Error(args...)
}

// Errorf logs at Error level with format.
func Errorf(format string, args ...any) {
	sugared.Errorf(format, args...)
}
