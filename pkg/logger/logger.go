package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New creates a production-ready zap logger.
func New() (*zap.Logger, error) {
	level := zap.InfoLevel
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		if err := level.UnmarshalText([]byte(v)); err != nil {
			return nil, err
		}
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	cfg := zap.Config{
		Level:            zap.NewAtomicLevelAt(level),
		Encoding:         "json",
		EncoderConfig:    encoderCfg,
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	return cfg.Build()
}
