package tts

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/domain"
)

// PiperClient implements usecase.TTSClient via Piper TTS HTTP server.
type PiperClient struct {
	endpoint   string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewPiperClient creates a new Piper TTS client.
func NewPiperClient(endpoint string, timeout time.Duration, logger *zap.Logger) *PiperClient {
	return &PiperClient{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

// Synthesize converts text to WAV audio using Piper TTS.
func (c *PiperClient) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	synthURL := fmt.Sprintf("%s/api/tts?text=%s", c.endpoint, url.QueryEscape(text))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, synthURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create tts request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", domain.ErrTTSUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("%w: status %d: %s", domain.ErrTTSUnavailable, resp.StatusCode, string(b))
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, resp.Body); err != nil {
		return nil, "", fmt.Errorf("read tts response: %w", err)
	}

	c.logger.Debug("tts synthesis completed", zap.Int("text_len", len(text)), zap.Int("audio_bytes", buf.Len()))
	return buf.Bytes(), "wav", nil
}

// HealthCheck verifies the Piper TTS server is responsive.
func (c *PiperClient) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrTTSUnavailable, err)
	}
	defer resp.Body.Close()
	return nil
}
