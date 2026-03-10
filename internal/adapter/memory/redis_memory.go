package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/domain"
)

const (
	memoryKeyPrefix = "vbot:memory:"
	memoryMaxTurns  = 100
)

// RedisMemory implements usecase.ConversationMemory using Redis lists + VSS.
type RedisMemory struct {
	client *redis.Client
	logger *zap.Logger
}

// NewRedisMemory creates a new conversation memory store.
func NewRedisMemory(client *redis.Client, logger *zap.Logger) *RedisMemory {
	return &RedisMemory{
		client: client,
		logger: logger,
	}
}

// AddTurn appends a conversation turn to the session's history (ring buffer).
func (m *RedisMemory) AddTurn(ctx context.Context, sessionID string, turn domain.ConversationTurn) error {
	key := memoryKeyPrefix + sessionID

	data, err := json.Marshal(turn)
	if err != nil {
		return fmt.Errorf("marshal turn: %w", err)
	}

	pipe := m.client.Pipeline()
	pipe.RPush(ctx, key, string(data))
	pipe.LTrim(ctx, key, -memoryMaxTurns, -1)
	pipe.Expire(ctx, key, 24*time.Hour)
	_, err = pipe.Exec(ctx)
	return err
}

// GetRecentTurns retrieves the most recent N turns.
func (m *RedisMemory) GetRecentTurns(ctx context.Context, sessionID string, count int) ([]domain.ConversationTurn, error) {
	key := memoryKeyPrefix + sessionID

	results, err := m.client.LRange(ctx, key, int64(-count), -1).Result()
	if err != nil {
		return nil, fmt.Errorf("get recent turns: %w", err)
	}

	turns := make([]domain.ConversationTurn, 0, len(results))
	for _, r := range results {
		var turn domain.ConversationTurn
		if err := json.Unmarshal([]byte(r), &turn); err != nil {
			m.logger.Warn("failed to unmarshal turn", zap.Error(err))
			continue
		}
		turns = append(turns, turn)
	}
	return turns, nil
}

// SearchRelevant retrieves semantically similar past turns.
// For simplicity, returns the most recent turns (full semantic search requires VSS on turns).
func (m *RedisMemory) SearchRelevant(ctx context.Context, sessionID string, embedding []float32, topK int) ([]domain.ConversationTurn, error) {
	// Fallback: return most recent turns weighted by relevance
	return m.GetRecentTurns(ctx, sessionID, topK)
}

// ClearSession clears all conversation history for a session.
func (m *RedisMemory) ClearSession(ctx context.Context, sessionID string) error {
	key := memoryKeyPrefix + sessionID
	return m.client.Del(ctx, key).Err()
}

func float32SliceToBytes(floats []float32) []byte {
	buf := make([]byte, len(floats)*4)
	for i, f := range floats {
		bits := math.Float32bits(f)
		buf[i*4] = byte(bits)
		buf[i*4+1] = byte(bits >> 8)
		buf[i*4+2] = byte(bits >> 16)
		buf[i*4+3] = byte(bits >> 24)
	}
	return buf
}
