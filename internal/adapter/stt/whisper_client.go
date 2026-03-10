package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/domain"
)

// WhisperClient implements usecase.STTClient via whisper.cpp HTTP server.
type WhisperClient struct {
	endpoint   string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewWhisperClient creates a new whisper.cpp server client.
func NewWhisperClient(endpoint string, timeout time.Duration, logger *zap.Logger) *WhisperClient {
	return &WhisperClient{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

type whisperResponse struct {
	Text     string           `json:"text"`
	Segments []whisperSegment `json:"segments"`
}

type whisperSegment struct {
	Text  string  `json:"text"`
	Start float64 `json:"t0"`
	End   float64 `json:"t1"`
}

// Transcribe sends an audio file to the whisper.cpp server and returns segments.
func (c *WhisperClient) Transcribe(ctx context.Context, audioPath string) ([]domain.TranscriptSegment, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return nil, fmt.Errorf("%w: open audio file: %v", domain.ErrSTTUnavailable, err)
	}
	defer f.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", audioPath)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("copy file to form: %w", err)
	}

	_ = writer.WriteField("response_format", "json")
	_ = writer.WriteField("temperature", "0.0")

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	url := fmt.Sprintf("%s/inference", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrSTTUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: status %d: %s", domain.ErrSTTUnavailable, resp.StatusCode, string(b))
	}

	var whisperResp whisperResponse
	if err := json.NewDecoder(resp.Body).Decode(&whisperResp); err != nil {
		return nil, fmt.Errorf("decode whisper response: %w", err)
	}

	segments := make([]domain.TranscriptSegment, 0, len(whisperResp.Segments))
	for _, s := range whisperResp.Segments {
		segments = append(segments, domain.TranscriptSegment{
			Text:      s.Text,
			StartTime: s.Start,
			EndTime:   s.End,
		})
	}

	c.logger.Debug("transcription completed", zap.String("file", audioPath), zap.Int("segments", len(segments)))
	return segments, nil
}

// HealthCheck pings the whisper server.
func (c *WhisperClient) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrSTTUnavailable, err)
	}
	defer resp.Body.Close()
	return nil
}
