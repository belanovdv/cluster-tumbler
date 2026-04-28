package logging

import (
	"errors"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Config struct {
	Level   string `yaml:"level"`
	Format  string `yaml:"format"`
	Console bool   `yaml:"console"`
	File    string `yaml:"file"`
}

func New(cfg Config) (*zap.Logger, error) {
	level := zapcore.DebugLevel
	if cfg.Level != "" {
		if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
			level = zapcore.DebugLevel
		}
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "ts",
		LevelKey:       "level",
		MessageKey:     "msg",
		NameKey:        "",
		CallerKey:      "",
		FunctionKey:    "",
		StacktraceKey:  "",
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	}

	var encoder zapcore.Encoder
	switch cfg.Format {
	case "json":
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	default:
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	var sinks []zapcore.WriteSyncer

	if cfg.Console || cfg.File == "" {
		sinks = append(sinks, zapcore.AddSync(os.Stdout))
	}

	if cfg.File != "" {
		file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, zapcore.AddSync(file))
	}

	if len(sinks) == 0 {
		return nil, errors.New("logger has no output sinks")
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.NewMultiWriteSyncer(sinks...),
		level,
	)

	return zap.New(core), nil
}

func WithComponent(base *zap.Logger, component string) *zap.Logger {
	return base.With(zap.String("component", component))
}
