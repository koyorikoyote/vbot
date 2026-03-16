package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/domain"
)

// EmbeddingClient implements usecase.EmbeddingClient via Ollama's embedding API.
type EmbeddingClient struct {
	endpoint   string
	model      string
	httpClient *http.Client
	logger     *zap.Logger
}

// NewEmbeddingClient creates a new embedding client.
func NewEmbeddingClient(endpoint, model string, timeout time.Duration, logger *zap.Logger) *EmbeddingClient {
	return &EmbeddingClient{
		endpoint: endpoint,
		model:    model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed generates a vector embedding for the given text.
func (c *EmbeddingClient) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := embedRequest{
		Model: c.model,
		Input: text,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	url := fmt.Sprintf("%s/api/embed", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrLLMUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: status %d: %s", domain.ErrLLMUnavailable, resp.StatusCode, string(b))
	}

	var embedResp embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, fmt.Errorf("%w: decode embedding response: %v", domain.ErrLLMResponseInvalid, err)
	}

	if len(embedResp.Embeddings) == 0 || len(embedResp.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("%w: empty embedding", domain.ErrLLMResponseInvalid)
	}

	c.logger.Debug("embedding generated", zap.Int("dimension", len(embedResp.Embeddings[0])))
	return embedResp.Embeddings[0], nil
}
// Warmup sends an empty request to Ollama to ensure the embedding model is loaded in memory.
func (c *EmbeddingClient) Warmup(ctx context.Context) error {
	reqBody := embedRequest{
		Model: c.model,
		Input: "",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal embedding warmup request: %w", err)
	}

	url := fmt.Sprintf("%s/api/embed", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("create embedding warmup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	c.logger.Info("warming up embedding model", zap.String("model", c.model))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("embedding warmup failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("embedding warmup status %d: %s", resp.StatusCode, string(b))
	}

	c.logger.Info("embedding model warmed up", zap.String("model", c.model))
	return nil
}
