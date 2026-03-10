package usecase

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/domain"
)

// SemanticCacheUseCase manages the semantic cache.
type SemanticCacheUseCase struct {
	cache    SemanticCacheRepository
	embedder EmbeddingClient
	ttl      time.Duration
	threshold float64
	logger   *zap.Logger
}

// NewSemanticCacheUseCase creates a new semantic cache use case.
func NewSemanticCacheUseCase(cache SemanticCacheRepository, embedder EmbeddingClient, ttl time.Duration, threshold float64, logger *zap.Logger) *SemanticCacheUseCase {
	return &SemanticCacheUseCase{
		cache:     cache,
		embedder:  embedder,
		ttl:       ttl,
		threshold: threshold,
		logger:    logger,
	}
}

// Lookup checks the cache for a similar query.
func (s *SemanticCacheUseCase) Lookup(ctx context.Context, input string) (string, bool, []float32, error) {
	embedding, err := s.embedder.Embed(ctx, input)
	if err != nil {
		return "", false, nil, fmt.Errorf("embed for cache: %w", err)
	}

	cached, hit, err := s.cache.FindSimilar(ctx, embedding, s.threshold)
	if err != nil {
		return "", false, embedding, nil // cache miss on error, pass embedding through
	}

	if hit {
		s.logger.Debug("semantic cache hit")
	}

	return cached, hit, embedding, nil
}

// Store saves a response in the cache.
func (s *SemanticCacheUseCase) Store(ctx context.Context, embedding []float32, response string) error {
	return s.cache.Store(ctx, embedding, response, s.ttl)
}

// StoreWithInput embeds and stores a response.
func (s *SemanticCacheUseCase) StoreWithInput(ctx context.Context, input string, response string) error {
	embedding, err := s.embedder.Embed(ctx, input)
	if err != nil {
		return fmt.Errorf("embed for cache store: %w", err)
	}
	return s.cache.Store(ctx, embedding, response, s.ttl)
}

// Warm pre-populates the cache with common responses.
func (s *SemanticCacheUseCase) Warm(ctx context.Context, entries map[string]string) error {
	for input, response := range entries {
		if err := s.StoreWithInput(ctx, input, response); err != nil {
			s.logger.Warn("failed to warm cache entry", zap.String("input", input), zap.Error(err))
		}
	}
	return nil
}

// unused import guard
var _ = domain.ErrLLMUnavailable
