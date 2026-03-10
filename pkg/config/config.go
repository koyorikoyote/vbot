package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all application configuration.
type Config struct {
	Server    ServerConfig
	LLM       LLMConfig
	Embedding EmbeddingConfig
	Redis     RedisConfig
	TTS       TTSConfig
	STT       STTConfig
	Ingest    IngestConfig
	VBot      VBotConfig
}

type ServerConfig struct {
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type LLMConfig struct {
	Endpoint   string
	ModelName  string
	MaxTokens  int
	Timeout    time.Duration
	SystemRole string
}

type EmbeddingConfig struct {
	Endpoint  string
	ModelName string
	Dimension int
	Timeout   time.Duration
}

type RedisConfig struct {
	URL      string
	Password string
	DB       int
}

type TTSConfig struct {
	PiperEndpoint string
	PiperTimeout  time.Duration
	UseSAPI       bool // force Windows SAPI even if Piper is available
}

type STTConfig struct {
	WhisperEndpoint string
	WhisperModel    string
	WhisperTimeout  time.Duration
	FFmpegPath      string
	FFprobePath     string
}

type IngestConfig struct {
	ChunkDurationMin     int
	OverlapDurationSec   int
	WorkerPoolSize       int
	TraitDedupeThreshold float64
	UploadDir            string
	MaxFileSizeMB        int
}

type VBotConfig struct {
	PersonalityFile       string
	RAGTopK               int
	RAGScoreThreshold     float64
	CacheTTL              time.Duration
	CacheSimilarityThresh float64
	MemoryWindowSize      int
}

// Load reads configuration from environment variables with defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port:         envInt("SERVER_PORT", 8090),
			ReadTimeout:  envDuration("SERVER_READ_TIMEOUT", 30*time.Second),
			WriteTimeout: envDuration("SERVER_WRITE_TIMEOUT", 60*time.Second),
		},
		LLM: LLMConfig{
			Endpoint:  envStr("VLLM_ENDPOINT", "http://localhost:11434"),
			ModelName: envStr("VLLM_MODEL_NAME", "llama3.1:8b"),
			MaxTokens: envInt("LLM_MAX_TOKENS", 512),
			Timeout:   envDuration("LLM_TIMEOUT", 30*time.Second),
		},
		Embedding: EmbeddingConfig{
			Endpoint:  envStr("EMBEDDING_ENDPOINT", "http://localhost:11434"),
			ModelName: envStr("EMBEDDING_MODEL_NAME", "nomic-embed-text"),
			Dimension: envInt("EMBEDDING_DIMENSION", 768),
			Timeout:   envDuration("EMBEDDING_TIMEOUT", 10*time.Second),
		},
		Redis: RedisConfig{
			URL:      envStr("REDIS_URL", "localhost:6379"),
			Password: envStr("REDIS_PASSWORD", ""),
			DB:       envInt("REDIS_DB", 0),
		},
		TTS: TTSConfig{
			PiperEndpoint: envStr("TTS_ENDPOINT", "http://localhost:5000"),
			PiperTimeout:  envDuration("TTS_TIMEOUT", 15*time.Second),
			UseSAPI:       envBool("TTS_FORCE_SAPI", false),
		},
		STT: STTConfig{
			WhisperEndpoint: envStr("WHISPER_ENDPOINT", "http://localhost:9000"),
			WhisperModel:    envStr("WHISPER_MODEL", "ggml-medium.bin"),
			WhisperTimeout:  envDuration("WHISPER_TIMEOUT", 10*time.Minute),
			FFmpegPath:      envStr("FFMPEG_PATH", "ffmpeg"),
			FFprobePath:     envStr("FFPROBE_PATH", "ffprobe"),
		},
		Ingest: IngestConfig{
			ChunkDurationMin:     envInt("INGEST_CHUNK_DURATION_MIN", 10),
			OverlapDurationSec:   envInt("INGEST_OVERLAP_SEC", 10),
			WorkerPoolSize:       envInt("INGEST_WORKERS", 2),
			TraitDedupeThreshold: envFloat("INGEST_DEDUPE_THRESHOLD", 0.90),
			UploadDir:            envStr("INGEST_UPLOAD_DIR", "data/uploads"),
			MaxFileSizeMB:        envInt("INGEST_MAX_FILE_SIZE_MB", 4096),
		},
		VBot: VBotConfig{
			PersonalityFile:       envStr("VBOT_PERSONALITY_FILE", "data/personalities/immergold.yaml"),
			RAGTopK:               envInt("VBOT_RAG_TOP_K", 5),
			RAGScoreThreshold:     envFloat("VBOT_RAG_SCORE_THRESHOLD", 0.70),
			CacheTTL:              envDuration("VBOT_CACHE_TTL", 30*time.Minute),
			CacheSimilarityThresh: envFloat("VBOT_CACHE_SIMILARITY_THRESHOLD", 0.92),
			MemoryWindowSize:      envInt("VBOT_MEMORY_WINDOW_SIZE", 12),
		},
	}

	if cfg.LLM.Endpoint == "" {
		return nil, fmt.Errorf("VLLM_ENDPOINT is required")
	}

	return cfg, nil
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
