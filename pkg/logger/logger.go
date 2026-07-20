// Package logger configures the process-wide zap logger and package wrappers.
package logger

import (
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	lifecycleMu   sync.RWMutex
	globalLogger  = zap.NewNop().Sugar()
	globalLogFile *guardedLogFile
	restoreGlobal func()
)

type guardedLogFile struct {
	mu     sync.Mutex
	writer *lumberjack.Logger
	closed bool
}

func (w *guardedLogFile) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return len(data), nil
	}
	return w.writer.Write(data)
}

func (w *guardedLogFile) Sync() error {
	return nil
}

func (w *guardedLogFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.writer.Close()
}

// Init configures console output and, when filename is non-empty, a rotating
// JSON file. Calling Init again releases the previous file writer first.
func Init(level, filename string, maxSize, maxBackups, maxAge int) {
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	zapLevel := parseLevel(level)
	cores := []zapcore.Core{zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(os.Stdout),
		zapLevel,
	)}

	var logFile *guardedLogFile
	if filename != "" {
		logFile = &guardedLogFile{
			writer: &lumberjack.Logger{
				Filename:   filename,
				MaxSize:    maxSize,
				MaxBackups: maxBackups,
				MaxAge:     maxAge,
				Compress:   true,
			},
		}
		fileEncoderConfig := encoderConfig
		fileEncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		cores = append(cores, zapcore.NewCore(
			zapcore.NewJSONEncoder(fileEncoderConfig),
			zapcore.AddSync(logFile),
			zapLevel,
		))
	}

	rawLogger := zap.New(
		zapcore.NewTee(cores...),
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)
	logger := rawLogger.WithOptions(zap.AddCallerSkip(1)).Sugar()

	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	previousLogger := globalLogger
	previousLogFile := globalLogFile
	globalLogger = logger
	globalLogFile = logFile
	if restoreGlobal == nil {
		restoreGlobal = zap.ReplaceGlobals(rawLogger)
	} else {
		zap.ReplaceGlobals(rawLogger)
	}
	_ = previousLogger.Sync()
	if previousLogFile != nil {
		_ = previousLogFile.Close()
	}
}

func parseLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

func Debug(args ...interface{}) {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	globalLogger.Debug(args...)
}

func Debugf(template string, args ...interface{}) {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	globalLogger.Debugf(template, args...)
}

func Info(args ...interface{}) {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	globalLogger.Info(args...)
}

func Infof(template string, args ...interface{}) {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	globalLogger.Infof(template, args...)
}

func Warn(args ...interface{}) {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	globalLogger.Warn(args...)
}

func Warnf(template string, args ...interface{}) {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	globalLogger.Warnf(template, args...)
}

func Error(args ...interface{}) {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	globalLogger.Error(args...)
}

func Errorf(template string, args ...interface{}) {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	globalLogger.Errorf(template, args...)
}

func Fatal(args ...interface{}) {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	globalLogger.Fatal(args...)
}

func Fatalf(template string, args ...interface{}) {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	globalLogger.Fatalf(template, args...)
}

// Sync flushes the active logger.
func Sync() {
	lifecycleMu.RLock()
	defer lifecycleMu.RUnlock()
	_ = globalLogger.Sync()
}

// Close flushes logs, releases the file writer, and is safe to call repeatedly.
func Close() {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	closeCurrentLocked()
}

func closeCurrentLocked() {
	logger := globalLogger
	logFile := globalLogFile
	globalLogger = zap.NewNop().Sugar()
	globalLogFile = nil
	if restoreGlobal != nil {
		restoreGlobal()
		restoreGlobal = nil
	}
	_ = logger.Sync()
	if logFile != nil {
		_ = logFile.Close()
	}
}
