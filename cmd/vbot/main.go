package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	adapthttp "github.com/vbot/vbot/internal/adapter/http"
	"github.com/vbot/vbot/internal/adapter/cache"
	"github.com/vbot/vbot/internal/adapter/llm"
	"github.com/vbot/vbot/internal/adapter/memory"
	"github.com/vbot/vbot/internal/adapter/rag"
	"github.com/vbot/vbot/internal/adapter/stt"
	"github.com/vbot/vbot/internal/adapter/tts"
	"github.com/vbot/vbot/internal/adapter/ws"
	"github.com/vbot/vbot/internal/domain"
	"github.com/vbot/vbot/internal/usecase"
	"github.com/vbot/vbot/pkg/config"
	"github.com/vbot/vbot/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	zapLogger, err := logger.New()
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}
	defer zapLogger.Sync()

	zapLogger.Info("starting vbot", zap.Int("port", cfg.Server.Port))

	// Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.URL,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer redisClient.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		zapLogger.Warn("redis not available", zap.Error(err))
	}

	// Adapters
	ollamaClient := llm.NewOllamaClient(
		cfg.LLM.Endpoint,
		cfg.LLM.ModelName,
		cfg.LLM.NumTokens,
		cfg.LLM.Timeout,
		cfg.LLM.NumThreads,
		zapLogger,
	)
	embeddingClient := llm.NewEmbeddingClient(cfg.Embedding.Endpoint, cfg.Embedding.ModelName, cfg.Embedding.Timeout, zapLogger)
	whisperClient := stt.NewWhisperClient(cfg.STT.WhisperEndpoint, cfg.STT.WhisperTimeout, zapLogger)
	ffmpegProc := stt.NewFFmpegProcessor(cfg.STT.FFmpegPath, cfg.STT.FFprobePath, cfg.Ingest.UploadDir, zapLogger)
	redisRAG := rag.NewRedisRAG(redisClient, embeddingClient, cfg.Embedding.Dimension, zapLogger)
	if err := redisRAG.EnsureIndex(ctx); err != nil {
		zapLogger.Warn("failed to ensure RAG index", zap.Error(err))
	}
	go loadDefaultTraits(ctx, redisRAG, embeddingClient, zapLogger)

	redisMemory := memory.NewRedisMemory(redisClient, zapLogger)
	redisCache := cache.NewRedisCache(redisClient, cfg.Embedding.Dimension, zapLogger)
	if err := redisCache.EnsureIndex(ctx); err != nil {
		zapLogger.Warn("failed to ensure cache index", zap.Error(err))
	}

	piperClient := tts.NewPiperClient(cfg.TTS.PiperEndpoint, cfg.TTS.PiperTimeout, zapLogger)
	sapiClient := tts.NewSAPIClient(cfg.Ingest.UploadDir, zapLogger)
	ttsChain := tts.NewTTSChain(piperClient, sapiClient, cfg.TTS.UseSAPI, zapLogger)

	hub := ws.NewHub(zapLogger)
	go hub.Run(ctx)

	traitExtractor := rag.NewLLMTraitExtractor(ollamaClient, zapLogger)

	// Use cases
	vbotEngine := usecase.NewVBotEngine(
		ollamaClient, embeddingClient, redisRAG, redisMemory, redisCache, ttsChain, hub,
		usecase.VBotEngineConfig{
			RAGTopK:          cfg.VBot.RAGTopK,
			RAGThreshold:     cfg.VBot.RAGScoreThreshold,
			CacheThreshold:   cfg.VBot.CacheSimilarityThresh,
			CacheTTL:         cfg.VBot.CacheTTL,
			MemoryWindowSize: cfg.VBot.MemoryWindowSize,
		},
		zapLogger,
	)
	vbotEngine.Warmup(ctx)

	ingestEngine := usecase.NewIngestEngine(
		ffmpegProc, whisperClient, traitExtractor, redisRAG, embeddingClient,
		usecase.IngestEngineConfig{
			ChunkDuration:   time.Duration(cfg.Ingest.ChunkDurationMin) * time.Minute,
			OverlapDuration: time.Duration(cfg.Ingest.OverlapDurationSec) * time.Second,
			WorkerPoolSize:  cfg.Ingest.WorkerPoolSize,
			DedupeThreshold: cfg.Ingest.TraitDedupeThreshold,
		},
		zapLogger,
	)

	// HTTP
	os.MkdirAll(cfg.Ingest.UploadDir, 0755)
	router := adapthttp.NewRouter(vbotEngine, ingestEngine, redisRAG, hub, cfg.Ingest.UploadDir, cfg.Ingest.MaxFileSizeMB, ctx, zapLogger)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router.Handler(),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	zapLogger.Info("tts backend active", zap.String("backend", ttsChain.ActiveBackend(ctx)))

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		zapLogger.Info("shutting down...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	zapLogger.Info("vbot listening", zap.String("addr", fmt.Sprintf(":%d", cfg.Server.Port)))
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		zapLogger.Fatal("server failed", zap.Error(err))
	}
}

func loadDefaultTraits(ctx context.Context, ragRepo *rag.RedisRAG, embedder *llm.EmbeddingClient, zapLogger *zap.Logger) {
	defaults := rag.DefaultImmergoldTraits()
	loaded := 0

	type result struct {
		trait domain.PersonalityTrait
		err   error
	}

	traitChan := make(chan domain.PersonalityTrait, len(defaults))
	resultChan := make(chan result, len(defaults))

	// Worker pool for parallel embedding
	numWorkers := 5
	for w := 0; w < numWorkers; w++ {
		go func() {
			for t := range traitChan {
				// Check if already exists to avoid redundant embedding
				exists, err := ragRepo.Exists(ctx, t.TraitID)
				if err == nil && exists {
					resultChan <- result{trait: t}
					continue
				}

				embedding, err := embedder.Embed(ctx, t.Content)
				if err != nil {
					resultChan <- result{err: err}
					continue
				}
				t.Embedding = embedding
				if err := ragRepo.StoreTrait(ctx, t); err != nil {
					resultChan <- result{err: err}
					continue
				}
				resultChan <- result{trait: t}
			}
		}()
	}

	// Send traits to workers
	for _, t := range defaults {
		traitChan <- t
	}
	close(traitChan)

	// Collect results
	for i := 0; i < len(defaults); i++ {
		res := <-resultChan
		if res.err == nil {
			loaded++
		}
	}

	zapLogger.Info("default traits loaded", zap.Int("count", loaded))
}
