// Package logger 提供统一的日志能力，基于 uber-go/zap 封装。
//
// 为什么选择 zap？
//   - 性能极高：零分配的结构化日志，比 logrus 快 4-10 倍
//   - 游戏服务器日志量大，性能很重要
//   - 支持文件轮转（通过 lumberjack），不用自己写日志切割逻辑
//
// 使用方式：
//
//	logger.Init("debug", "logs/server.log", 200, 7, 30)
//	logger.Info("玩家登录", zap.Int64("uid", 12345))
//	logger.Error("数据库连接失败", zap.Error(err))
package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// 全局 logger 实例，所有包通过 logger.Info/Error 等函数使用
var globalLogger *zap.SugaredLogger
var globalLogFile *lumberjack.Logger

// Init 初始化日志系统。
//
// 参数：
//   - level: 日志级别 "debug"/"info"/"warn"/"error"
//   - filename: 日志文件路径，为空则只输出到控制台
//   - maxSize: 单个日志文件最大 MB
//   - maxBackups: 保留的旧日志文件数量
//   - maxAge: 保留旧日志文件的最大天数
func Init(level, filename string, maxSize, maxBackups, maxAge int) {
	if globalLogFile != nil {
		_ = globalLogFile.Close()
		globalLogFile = nil
	}

	// 1. 解析日志级别
	zapLevel := parseLevel(level)

	// 2. 创建核心编码器配置
	// 游戏服务器日志需要精确到毫秒，方便排查时序问题
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder, // 控制台用彩色大写
		EncodeTime:     zapcore.ISO8601TimeEncoder,       // ISO8601 格式时间
		EncodeDuration: zapcore.MillisDurationEncoder,    // 毫秒级耗时
		EncodeCaller:   zapcore.ShortCallerEncoder,       // 短路径：pkg/file.go:123
	}

	// 3. 构建输出目标（cores）
	var cores []zapcore.Core

	// 控制台输出 —— 开发时直接在终端看日志
	consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)
	consoleCore := zapcore.NewCore(
		consoleEncoder,
		zapcore.AddSync(os.Stdout),
		zapLevel,
	)
	cores = append(cores, consoleCore)

	// 文件输出 —— 生产环境必须落盘，方便回溯问题
	if filename != "" {
		// lumberjack 负责日志文件的轮转、压缩、清理
		fileWriter := &lumberjack.Logger{
			Filename:   filename,
			MaxSize:    maxSize,
			MaxBackups: maxBackups,
			MaxAge:     maxAge,
			Compress:   true, // 旧日志自动 gzip 压缩，节省磁盘
		}
		// 文件用 JSON 格式，方便 ELK/Loki 等日志系统采集
		fileEncoderConfig := encoderConfig
		fileEncoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder // 文件中不需要颜色
		fileEncoder := zapcore.NewJSONEncoder(fileEncoderConfig)
		fileCore := zapcore.NewCore(
			fileEncoder,
			zapcore.AddSync(fileWriter),
			zapLevel,
		)
		cores = append(cores, fileCore)
		globalLogFile = fileWriter
	}

	// 4. 组合多个 Core，创建最终 Logger
	// zapcore.NewTee 类似 Linux 的 tee 命令，同时输出到多个目标
	combinedCore := zapcore.NewTee(cores...)

	// The raw logger serves zap.L(), so it keeps the original caller frame.
	rawLogger := zap.New(combinedCore,
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel), // Error 及以上级别自动附加堆栈
	)

	// SugaredLogger 提供 printf 风格的 API，使用更方便
	// 性能略低于 Logger，但对游戏服务器的日志量来说完全可以接受
	globalLogger = rawLogger.WithOptions(zap.AddCallerSkip(1)).Sugar()
	zap.ReplaceGlobals(rawLogger)
}

// parseLevel 将字符串日志级别转换为 zapcore.Level
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
		return zapcore.InfoLevel // 默认 info 级别
	}
}

// ========== 以下是日志输出函数，直接委托给全局 SugaredLogger ==========

func Debug(args ...interface{})                   { globalLogger.Debug(args...) }
func Debugf(template string, args ...interface{}) { globalLogger.Debugf(template, args...) }
func Info(args ...interface{})                    { globalLogger.Info(args...) }
func Infof(template string, args ...interface{})  { globalLogger.Infof(template, args...) }
func Warn(args ...interface{})                    { globalLogger.Warn(args...) }
func Warnf(template string, args ...interface{})  { globalLogger.Warnf(template, args...) }
func Error(args ...interface{})                   { globalLogger.Error(args...) }
func Errorf(template string, args ...interface{}) { globalLogger.Errorf(template, args...) }
func Fatal(args ...interface{})                   { globalLogger.Fatal(args...) }
func Fatalf(template string, args ...interface{}) { globalLogger.Fatalf(template, args...) }

// Sync 刷新日志缓冲区，程序退出前应调用
// 确保所有未写入的日志都被持久化到磁盘
func Sync() {
	if globalLogger != nil {
		_ = globalLogger.Sync()
	}
}

// Close flushes logs and releases the active file writer.
func Close() {
	Sync()
	if globalLogFile != nil {
		_ = globalLogFile.Close()
		globalLogFile = nil
	}
}
