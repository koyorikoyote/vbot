package cache

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	cacheIndexName = "idx:vbot:cache"
	cacheKeyPrefix = "vbot:cache:"
)

// RedisCache implements usecase.SemanticCacheRepository using Redis VSS.
type RedisCache struct {
	client *redis.Client
	dim    int
	logger *zap.Logger
}

// NewRedisCache creates a new semantic cache.
func NewRedisCache(client *redis.Client, dimension int, logger *zap.Logger) *RedisCache {
	return &RedisCache{
		client: client,
		dim:    dimension,
		logger: logger,
	}
}

// EnsureIndex creates the cache VSS index if it doesn't exist.
func (c *RedisCache) EnsureIndex(ctx context.Context) error {
	_, err := c.client.Do(ctx, "FT.INFO", cacheIndexName).Result()
	if err == nil {
		return nil
	}

	args := []interface{}{
		"FT.CREATE", cacheIndexName,
		"ON", "HASH",
		"PREFIX", "1", cacheKeyPrefix,
		"SCHEMA",
		"response", "TEXT",
		"embedding", "VECTOR", "HNSW", "6",
		"TYPE", "FLOAT32",
		"DIM", c.dim,
		"DISTANCE_METRIC", "COSINE",
	}

	_, err = c.client.Do(ctx, args...).Result()
	if err != nil {
		return fmt.Errorf("create cache index: %w", err)
	}

	c.logger.Info("cache index created", zap.String("index", cacheIndexName))
	return nil
}

// FindSimilar looks for a cached response with similarity above threshold.
func (c *RedisCache) FindSimilar(ctx context.Context, embedding []float32, threshold float64) (string, bool, error) {
	embBytes := float32SliceToBytes(embedding)

	query := "*=>[KNN 1 @embedding $vec AS score]"
	args := []interface{}{
		"FT.SEARCH", cacheIndexName, query,
		"PARAMS", "2", "vec", embBytes,
		"SORTBY", "score",
		"RETURN", "2", "response", "score",
		"LIMIT", "0", "1",
		"DIALECT", "2",
	}

	result, err := c.client.Do(ctx, args...).Result()
	if err != nil {
		return "", false, fmt.Errorf("search cache: %w", err)
	}

	arr, ok := result.([]interface{})
	if !ok || len(arr) < 3 {
		return "", false, nil
	}

	fields, ok := arr[2].([]interface{})
	if !ok || len(fields) < 4 {
		return "", false, nil
	}

	fieldMap := make(map[string]string)
	for j := 0; j < len(fields)-1; j += 2 {
		key, _ := fields[j].(string)
		val, _ := fields[j+1].(string)
		fieldMap[key] = val
	}

	score, err := strconv.ParseFloat(fieldMap["score"], 64)
	if err != nil {
		return "", false, nil
	}

	// Redis cosine distance: 0 = identical, 2 = opposite
	// Similarity = 1 - distance
	similarity := 1 - score
	if similarity < threshold {
		return "", false, nil
	}

	c.logger.Debug("cache hit", zap.Float64("similarity", similarity))
	return fieldMap["response"], true, nil
}

// Store saves a response with its embedding in the cache.
func (c *RedisCache) Store(ctx context.Context, embedding []float32, response string, ttl time.Duration) error {
	key := fmt.Sprintf("%s%d", cacheKeyPrefix, time.Now().UnixNano())
	embBytes := float32SliceToBytes(embedding)

	pipe := c.client.Pipeline()
	pipe.HSet(ctx, key, map[string]interface{}{
		"response":  response,
		"embedding": embBytes,
	})
	if ttl > 0 {
		pipe.Expire(ctx, key, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
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
