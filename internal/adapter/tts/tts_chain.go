package tts

import (
	"context"

	"go.uber.org/zap"
)

// TTSChain implements usecase.TTSClient with a fallback chain.
type TTSChain struct {
	primary  *PiperClient
	fallback *SAPIClient
	useSAPI  bool // force SAPI regardless of Piper availability
	logger   *zap.Logger
}

// NewTTSChain creates a TTS client with automatic fallback.
func NewTTSChain(primary *PiperClient, fallback *SAPIClient, forceSAPI bool, logger *zap.Logger) *TTSChain {
	return &TTSChain{
		primary:  primary,
		fallback: fallback,
		useSAPI:  forceSAPI,
		logger:   logger,
	}
}

// Synthesize tries Piper first, falls back to Windows SAPI.
func (c *TTSChain) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	if c.useSAPI {
		c.logger.Debug("using sapi (forced)")
		return c.fallback.Synthesize(ctx, text)
	}

	// Try Piper first
	if err := c.primary.HealthCheck(ctx); err == nil {
		audio, format, err := c.primary.Synthesize(ctx, text)
		if err == nil {
			return audio, format, nil
		}
		c.logger.Warn("piper synthesis failed, falling back to sapi", zap.Error(err))
	} else {
		c.logger.Debug("piper unavailable, using sapi fallback", zap.Error(err))
	}

	return c.fallback.Synthesize(ctx, text)
}

// HealthCheck checks if any TTS backend is available.
func (c *TTSChain) HealthCheck(ctx context.Context) error {
	if c.useSAPI {
		return c.fallback.HealthCheck(ctx)
	}

	if err := c.primary.HealthCheck(ctx); err == nil {
		return nil
	}
	return c.fallback.HealthCheck(ctx)
}

// ActiveBackend returns the name of the currently active TTS backend.
func (c *TTSChain) ActiveBackend(ctx context.Context) string {
	if c.useSAPI {
		return "sapi"
	}
	if err := c.primary.HealthCheck(ctx); err == nil {
		return "piper"
	}
	return "sapi"
}
