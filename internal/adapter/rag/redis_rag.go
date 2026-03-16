package rag

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/vbot/vbot/internal/domain"
	"github.com/vbot/vbot/internal/usecase"
)

const (
	ragIndexName = "idx:vbot:traits"
	ragKeyPrefix = "vbot:trait:"
)

// RedisRAG implements usecase.RAGRepository using Redis VSS.
type RedisRAG struct {
	client   *redis.Client
	embedder usecase.EmbeddingClient
	dim      int
	logger   *zap.Logger
}

// NewRedisRAG creates a new RAG repository.
func NewRedisRAG(client *redis.Client, embedder usecase.EmbeddingClient, dimension int, logger *zap.Logger) *RedisRAG {
	return &RedisRAG{
		client:   client,
		embedder: embedder,
		dim:      dimension,
		logger:   logger,
	}
}

// EnsureIndex creates the Redis VSS index if it doesn't exist.
func (r *RedisRAG) EnsureIndex(ctx context.Context) error {
	_, err := r.client.Do(ctx, "FT.INFO", ragIndexName).Result()
	if err == nil {
		return nil // index exists
	}

	args := []interface{}{
		"FT.CREATE", ragIndexName,
		"ON", "HASH",
		"PREFIX", "1", ragKeyPrefix,
		"SCHEMA",
		"trait_id", "TAG",
		"category", "TAG",
		"content", "TEXT",
		"evidence", "TEXT",
		"source_file", "TAG",
		"timestamp_range", "TEXT",
		"embedding", "VECTOR", "HNSW", "6",
		"TYPE", "FLOAT32",
		"DIM", r.dim,
		"DISTANCE_METRIC", "COSINE",
	}

	_, err = r.client.Do(ctx, args...).Result()
	if err != nil {
		return fmt.Errorf("create RAG index: %w", err)
	}

	r.logger.Info("rag index created", zap.String("index", ragIndexName))
	return nil
}

// StoreTrait stores a single personality trait.
func (r *RedisRAG) StoreTrait(ctx context.Context, trait domain.PersonalityTrait) error {
	key := ragKeyPrefix + trait.TraitID
	embBytes := float32SliceToBytes(trait.Embedding)

	_, err := r.client.HSet(ctx, key, map[string]interface{}{
		"trait_id":        trait.TraitID,
		"category":        trait.Category,
		"content":         trait.Content,
		"evidence":        trait.Evidence,
		"source_file":     trait.SourceFile,
		"timestamp_range": trait.TimestampRange,
		"embedding":       embBytes,
	}).Result()

	return err
}

// StoreTraitBatch stores multiple traits.
func (r *RedisRAG) StoreTraitBatch(ctx context.Context, traits []domain.PersonalityTrait) error {
	pipe := r.client.Pipeline()
	for _, trait := range traits {
		key := ragKeyPrefix + trait.TraitID
		embBytes := float32SliceToBytes(trait.Embedding)
		pipe.HSet(ctx, key, map[string]interface{}{
			"trait_id":        trait.TraitID,
			"category":        trait.Category,
			"content":         trait.Content,
			"evidence":        trait.Evidence,
			"source_file":     trait.SourceFile,
			"timestamp_range": trait.TimestampRange,
			"embedding":       embBytes,
		})
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("store trait batch: %w", err)
	}
	r.logger.Info("traits stored", zap.Int("count", len(traits)))
	return nil
}

// SearchTraits finds the most similar personality traits.
func (r *RedisRAG) SearchTraits(ctx context.Context, embedding []float32, topK int, threshold float64) ([]domain.PersonalityTrait, error) {
	embBytes := float32SliceToBytes(embedding)

	query := fmt.Sprintf("*=>[KNN %d @embedding $vec AS score]", topK)
	args := []interface{}{
		"FT.SEARCH", ragIndexName, query,
		"PARAMS", "2", "vec", embBytes,
		"SORTBY", "score",
		"RETURN", "7", "trait_id", "category", "content", "evidence", "source_file", "timestamp_range", "score",
		"DIALECT", "2",
	}

	result, err := r.client.Do(ctx, args...).Result()
	if err != nil {
		return nil, fmt.Errorf("search traits: %w", err)
	}

	return parseSearchResults(result, threshold)
}

// LoadFromFile loads personality traits from a YAML file and stores them.
func (r *RedisRAG) LoadFromFile(ctx context.Context, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("%w: %v", domain.ErrPersonalityNotFound, err)
	}

	var file personalityFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse personality file: %w", err)
	}

	for i := range file.Traits {
		trait := &file.Traits[i]
		if trait.TraitID == "" {
			trait.TraitID = fmt.Sprintf("yaml_%s_%d", trait.Category, i)
		}
		trait.SourceFile = filePath

		embedding, err := r.embedder.Embed(ctx, trait.Content)
		if err != nil {
			return fmt.Errorf("embed trait %s: %w", trait.TraitID, err)
		}
		trait.Embedding = embedding

		if err := r.StoreTrait(ctx, *trait); err != nil {
			return fmt.Errorf("store trait %s: %w", trait.TraitID, err)
		}
	}

	r.logger.Info("personality loaded from file", zap.String("file", filePath), zap.Int("traits", len(file.Traits)))
	return nil
}

// GetCompleteness calculates RAG personality coverage.
func (r *RedisRAG) GetCompleteness(ctx context.Context) (*domain.RAGCompleteness, error) {
	allTraits, err := r.AllTraits(ctx)
	if err != nil {
		return nil, err
	}

	categoryCounts := make(map[string]int)
	categorySources := make(map[string]map[string]bool)
	for _, t := range allTraits {
		categoryCounts[t.Category]++
		if categorySources[t.Category] == nil {
			categorySources[t.Category] = make(map[string]bool)
		}
		if t.SourceFile != "" {
			categorySources[t.Category]["uploaded"] = true
		} else {
			categorySources[t.Category]["default"] = true
		}
	}

	categories := make(map[string]domain.CategoryStatus)
	totalPct := 0
	categoryCount := 0

	for cat, target := range domain.CategoryTargets {
		count := categoryCounts[cat]
		pct := int(math.Min(float64(count)/float64(target)*100, 100))

		source := "default"
		if sources, ok := categorySources[cat]; ok {
			if sources["uploaded"] && sources["default"] {
				source = "mixed"
			} else if sources["uploaded"] {
				source = "uploaded"
			}
		}

		status := domain.CategoryStatus{
			TraitCount:   count,
			Target:       target,
			Completeness: pct,
			Source:       source,
		}

		if pct < 50 {
			status.Message = fmt.Sprintf("Upload stream recordings to improve %s knowledge", cat)
		}

		categories[cat] = status
		totalPct += pct
		categoryCount++
	}

	overall := 0
	if categoryCount > 0 {
		overall = totalPct / categoryCount
	}

	recommendation := ""
	if overall < 50 {
		recommendation = "Upload 2-3 stream recordings to build a solid personality base"
	} else if overall < 80 {
		recommendation = "Upload 1-2 more stream recordings to reach 80%+ coverage"
	} else if overall < 100 {
		recommendation = "Good coverage! Additional streams will further refine the personality"
	}

	return &domain.RAGCompleteness{
		OverallPercent: overall,
		Categories:     categories,
		Recommendation: recommendation,
	}, nil
}

// AllTraits retrieves all stored traits.
func (r *RedisRAG) AllTraits(ctx context.Context) ([]domain.PersonalityTrait, error) {
	query := "*"
	args := []interface{}{
		"FT.SEARCH", ragIndexName, query,
		"RETURN", "6", "trait_id", "category", "content", "evidence", "source_file", "timestamp_range",
		"LIMIT", "0", "1000",
	}

	result, err := r.client.Do(ctx, args...).Result()
	if err != nil {
		return nil, fmt.Errorf("list all traits: %w", err)
	}

	return parseSearchResults(result, 0)
}

// Exists checks if a trait already exists in the repository.
func (r *RedisRAG) Exists(ctx context.Context, traitID string) (bool, error) {
	key := ragKeyPrefix + traitID
	n, err := r.client.Exists(ctx, key).Result()
	return n > 0, err
}

type personalityFile struct {
	Traits []domain.PersonalityTrait `yaml:"traits"`
}

func parseSearchResults(result interface{}, threshold float64) ([]domain.PersonalityTrait, error) {
	arr, ok := result.([]interface{})
	if !ok || len(arr) < 1 {
		return nil, nil
	}

	var traits []domain.PersonalityTrait
	// arr[0] is total count, then pairs of (key, fields...)
	for i := 1; i < len(arr); i += 2 {
		if i+1 >= len(arr) {
			break
		}
		fields, ok := arr[i+1].([]interface{})
		if !ok {
			continue
		}

		trait := domain.PersonalityTrait{}
		fieldMap := make(map[string]string)
		for j := 0; j < len(fields)-1; j += 2 {
			key, _ := fields[j].(string)
			val, _ := fields[j+1].(string)
			fieldMap[key] = val
		}

		trait.TraitID = fieldMap["trait_id"]
		trait.Category = fieldMap["category"]
		trait.Content = fieldMap["content"]
		trait.Evidence = fieldMap["evidence"]
		trait.SourceFile = fieldMap["source_file"]
		trait.TimestampRange = fieldMap["timestamp_range"]

		// Filter by score threshold if present
		if scoreStr, exists := fieldMap["score"]; exists && threshold > 0 {
			score, err := strconv.ParseFloat(scoreStr, 64)
			if err == nil && score > threshold {
				continue
			}
		}

		traits = append(traits, trait)
	}

	return traits, nil
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
