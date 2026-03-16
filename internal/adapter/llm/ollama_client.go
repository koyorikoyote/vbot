package llm

import (
	"bufio"
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

// OllamaClient implements usecase.LLMClient via an OpenAI-compatible API.
type OllamaClient struct {
	endpoint   string
	model      string
	maxTokens  int
	httpClient *http.Client
	logger     *zap.Logger
}

// NewOllamaClient creates a new LLM client.
func NewOllamaClient(endpoint, model string, maxTokens int, timeout time.Duration, logger *zap.Logger) *OllamaClient {
	return &OllamaClient{
		endpoint:  endpoint,
		model:     model,
		maxTokens: maxTokens,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  chatOptions   `json:"options,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatOptions struct {
	NumPredict int `json:"num_predict,omitempty"`
	NumCtx     int `json:"num_ctx,omitempty"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
}

// Chat sends a conversation to the LLM and returns the assistant's reply.
func (c *OllamaClient) Chat(ctx context.Context, messages []domain.ConversationTurn, systemPrompt string) (string, error) {
	var msgs []chatMessage

	if systemPrompt != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: systemPrompt})
	}

	for _, m := range messages {
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
	}

	reqBody := chatRequest{
		Model:    c.model,
		Messages: msgs,
		Stream:   false,
		Options: chatOptions{
			NumPredict: c.maxTokens,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal chat request: %w", err)
	}

	url := fmt.Sprintf("%s/api/chat", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", domain.ErrLLMUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%w: status %d: %s", domain.ErrLLMUnavailable, resp.StatusCode, string(b))
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("%w: decode response: %v", domain.ErrLLMResponseInvalid, err)
	}

	if chatResp.Message.Content == "" {
		return "", fmt.Errorf("%w: empty response", domain.ErrLLMResponseInvalid)
	}

	c.logger.Debug("llm chat completed", zap.Int("input_messages", len(msgs)), zap.Int("output_len", len(chatResp.Message.Content)))
	return chatResp.Message.Content, nil
}

// ChatStream sends a conversation to the LLM and streams the assistant's reply back.
func (c *OllamaClient) ChatStream(ctx context.Context, messages []domain.ConversationTurn, systemPrompt string) (<-chan string, <-chan error) {
	outCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		defer close(outCh)
		defer close(errCh)

		var msgs []chatMessage
		if systemPrompt != "" {
			msgs = append(msgs, chatMessage{Role: "system", Content: systemPrompt})
		}
		for _, m := range messages {
			msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
		}

		reqBody := chatRequest{
			Model:    c.model,
			Messages: msgs,
			Stream:   true,
			Options: chatOptions{
				NumPredict: c.maxTokens,
				NumCtx:     1024,
			},
		}

		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			errCh <- fmt.Errorf("marshal chat request: %w", err)
			return
		}

		url := fmt.Sprintf("%s/api/chat", c.endpoint)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			errCh <- fmt.Errorf("create request: %w", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")

		// Important: bypass main httpClient to avoid standard Timeout interfering with the stream length
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			errCh <- fmt.Errorf("%w: %v", domain.ErrLLMUnavailable, err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			errCh <- fmt.Errorf("%w: status %d: %s", domain.ErrLLMUnavailable, resp.StatusCode, string(b))
			return
		}

		// Use bufio.Scanner instead of json.Decoder to avoid aggressive buffering and ensure swift TTFT
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var chunk chatResponse
			if err := json.Unmarshal(line, &chunk); err != nil {
				errCh <- fmt.Errorf("stream decode: %w (line: %s)", err, string(line))
				return
			}
			if chunk.Message.Content != "" {
				select {
				case <-ctx.Done():
					return
				case outCh <- chunk.Message.Content:
				}
			}
		}

		if err := scanner.Err(); err != nil && err != context.Canceled {
			errCh <- fmt.Errorf("stream read: %w", err)
		}
	}()

	return outCh, errCh
}
