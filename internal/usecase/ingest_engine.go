package usecase

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/domain"
)

// IngestEngine orchestrates the stream-to-RAG ingestion pipeline.
type IngestEngine struct {
	audio     AudioProcessor
	stt       STTClient
	extractor TraitExtractor
	rag       RAGRepository
	embedder  EmbeddingClient

	chunkDuration   time.Duration
	overlapDuration time.Duration
	workerPoolSize  int
	dedupeThreshold float64

	jobs   map[string]*domain.IngestionJob
	mu     sync.Mutex
	logger *zap.Logger
}

// IngestEngineConfig holds ingestion configuration.
type IngestEngineConfig struct {
	ChunkDuration   time.Duration
	OverlapDuration time.Duration
	WorkerPoolSize  int
	DedupeThreshold float64
}

// NewIngestEngine creates a new ingestion orchestrator.
func NewIngestEngine(
	audio AudioProcessor,
	stt STTClient,
	extractor TraitExtractor,
	rag RAGRepository,
	embedder EmbeddingClient,
	cfg IngestEngineConfig,
	logger *zap.Logger,
) *IngestEngine {
	return &IngestEngine{
		audio:           audio,
		stt:             stt,
		extractor:       extractor,
		rag:             rag,
		embedder:        embedder,
		chunkDuration:   cfg.ChunkDuration,
		overlapDuration: cfg.OverlapDuration,
		workerPoolSize:  cfg.WorkerPoolSize,
		dedupeThreshold: cfg.DedupeThreshold,
		jobs:            make(map[string]*domain.IngestionJob),
		logger:          logger,
	}
}

// IngestStream runs the full ingestion pipeline asynchronously.
func (e *IngestEngine) IngestStream(ctx context.Context, jobID, filePath string) {
	job := &domain.IngestionJob{
		JobID:      jobID,
		SourceFile: filePath,
		Status:     domain.StatusPending,
	}

	e.mu.Lock()
	e.jobs[jobID] = job
	e.mu.Unlock()

	go func() {
		if err := e.runPipeline(ctx, job); err != nil {
			e.mu.Lock()
			job.Status = domain.StatusError
			job.Error = err.Error()
			e.mu.Unlock()
			e.logger.Error("ingestion failed", zap.String("job_id", jobID), zap.Error(err))
		}
	}()
}

func (e *IngestEngine) runPipeline(ctx context.Context, job *domain.IngestionJob) error {
	// Step 1: Extract audio
	e.updateJob(job, domain.StatusExtractingAudio, 5)
	audioPath := job.SourceFile + ".wav"
	if err := e.audio.ExtractAudio(ctx, job.SourceFile, audioPath); err != nil {
		return fmt.Errorf("extract audio: %w", err)
	}

	// Step 2: Split into chunks
	e.updateJob(job, domain.StatusTranscribing, 10)
	chunkPaths, err := e.audio.SplitChunks(ctx, audioPath, e.chunkDuration, e.overlapDuration)
	if err != nil {
		return fmt.Errorf("split chunks: %w", err)
	}
	job.TotalChunks = len(chunkPaths)

	// Step 3: Transcribe chunks in parallel (bounded concurrency)
	type chunkResult struct {
		index    int
		segments []domain.TranscriptSegment
		err      error
	}
	results := make(chan chunkResult, len(chunkPaths))
	sem := make(chan struct{}, e.workerPoolSize)

	for i, path := range chunkPaths {
		sem <- struct{}{}
		go func(idx int, p string) {
			defer func() { <-sem }()
			segments, err := e.stt.Transcribe(ctx, p)
			results <- chunkResult{index: idx, segments: segments, err: err}
		}(i, path)
	}

	// Collect results
	allSegments := make([][]domain.TranscriptSegment, len(chunkPaths))
	for range chunkPaths {
		r := <-results
		if r.err != nil {
			e.logger.Warn("chunk transcription failed", zap.Int("chunk", r.index), zap.Error(r.err))
			continue
		}
		allSegments[r.index] = r.segments
		e.mu.Lock()
		job.DoneChunks++
		job.Progress = 10 + (job.DoneChunks*50)/job.TotalChunks
		e.mu.Unlock()
	}

	// Step 4: Merge transcripts
	mergedText := mergeTranscripts(allSegments)
	if mergedText == "" {
		return fmt.Errorf("%w: no transcript produced", domain.ErrIngestionFailed)
	}

	// Step 5: Extract traits
	e.updateJob(job, domain.StatusExtractingTraits, 65)
	sections := splitIntoSections(mergedText, 2000) // ~2000 chars per section

	var allTraits []domain.PersonalityTrait
	for i, section := range sections {
		startTime := float64(i) * 600 // approximate 10-min sections
		endTime := startTime + 600
		traits, err := e.extractor.ExtractTraits(ctx, section, job.SourceFile, startTime, endTime)
		if err != nil {
			e.logger.Warn("trait extraction failed for section", zap.Int("section", i), zap.Error(err))
			continue
		}
		allTraits = append(allTraits, traits...)
		e.mu.Lock()
		job.TraitsFound = len(allTraits)
		job.Progress = 65 + (i*25)/len(sections)
		e.mu.Unlock()
	}

	// Step 6: Embed and store traits
	e.updateJob(job, domain.StatusStoring, 90)
	for i := range allTraits {
		embedding, err := e.embedder.Embed(ctx, allTraits[i].Content)
		if err != nil {
			e.logger.Warn("failed to embed trait", zap.String("trait_id", allTraits[i].TraitID), zap.Error(err))
			continue
		}
		allTraits[i].Embedding = embedding
	}

	if err := e.rag.StoreTraitBatch(ctx, allTraits); err != nil {
		return fmt.Errorf("store traits: %w", err)
	}

	e.updateJob(job, domain.StatusDone, 100)
	e.logger.Info("ingestion completed", zap.String("job_id", job.JobID), zap.Int("traits", len(allTraits)))
	return nil
}

func (e *IngestEngine) updateJob(job *domain.IngestionJob, status string, progress int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	job.Status = status
	job.Progress = progress
}

// GetJob retrieves the status of an ingestion job.
func (e *IngestEngine) GetJob(jobID string) (*domain.IngestionJob, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	job, ok := e.jobs[jobID]
	if !ok {
		return nil, domain.ErrJobNotFound
	}
	return job, nil
}

// ListJobs returns all ingestion jobs.
func (e *IngestEngine) ListJobs() []*domain.IngestionJob {
	e.mu.Lock()
	defer e.mu.Unlock()
	jobs := make([]*domain.IngestionJob, 0, len(e.jobs))
	for _, j := range e.jobs {
		jobs = append(jobs, j)
	}
	return jobs
}

func mergeTranscripts(allSegments [][]domain.TranscriptSegment) string {
	var sb strings.Builder
	for _, segments := range allSegments {
		for _, s := range segments {
			text := strings.TrimSpace(s.Text)
			if text != "" {
				sb.WriteString(text)
				sb.WriteString(" ")
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

func splitIntoSections(text string, maxChars int) []string {
	words := strings.Fields(text)
	var sections []string
	var current strings.Builder

	for _, word := range words {
		if current.Len()+len(word)+1 > maxChars && current.Len() > 0 {
			sections = append(sections, current.String())
			current.Reset()
		}
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(word)
	}
	if current.Len() > 0 {
		sections = append(sections, current.String())
	}
	return sections
}
