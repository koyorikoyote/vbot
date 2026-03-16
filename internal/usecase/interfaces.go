package usecase

import (
	"context"
	"time"

	"github.com/vbot/vbot/internal/domain"
)

// LLMClient handles chat-style LLM inference.
type LLMClient interface {
	Chat(ctx context.Context, messages []domain.ConversationTurn, systemPrompt string) (string, error)
	ChatStream(ctx context.Context, messages []domain.ConversationTurn, systemPrompt string) (<-chan string, <-chan error)
	Warmup(ctx context.Context) error
}

// EmbeddingClient generates vector embeddings from text.
type EmbeddingClient interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Warmup(ctx context.Context) error
}

// STTClient transcribes an audio file to text segments.
type STTClient interface {
	Transcribe(ctx context.Context, audioPath string) ([]domain.TranscriptSegment, error)
}

// AudioProcessor extracts and chunks audio from video/audio files.
type AudioProcessor interface {
	ExtractAudio(ctx context.Context, inputPath, outputPath string) error
	SplitChunks(ctx context.Context, audioPath string, chunkDuration, overlapDuration time.Duration) ([]string, error)
	GetDuration(ctx context.Context, filePath string) (time.Duration, error)
}

// TraitExtractor extracts personality traits from transcript text using LLM.
type TraitExtractor interface {
	ExtractTraits(ctx context.Context, transcript string, sourceFile string, startTime, endTime float64) ([]domain.PersonalityTrait, error)
}

// RAGRepository stores and retrieves personality traits.
type RAGRepository interface {
	StoreTrait(ctx context.Context, trait domain.PersonalityTrait) error
	StoreTraitBatch(ctx context.Context, traits []domain.PersonalityTrait) error
	SearchTraits(ctx context.Context, embedding []float32, topK int, threshold float64) ([]domain.PersonalityTrait, error)
	LoadFromFile(ctx context.Context, filePath string) error
	GetCompleteness(ctx context.Context) (*domain.RAGCompleteness, error)
	AllTraits(ctx context.Context) ([]domain.PersonalityTrait, error)
	Exists(ctx context.Context, traitID string) (bool, error)
}

// ConversationMemory manages chat history per session.
type ConversationMemory interface {
	AddTurn(ctx context.Context, sessionID string, turn domain.ConversationTurn) error
	GetRecentTurns(ctx context.Context, sessionID string, count int) ([]domain.ConversationTurn, error)
	SearchRelevant(ctx context.Context, sessionID string, embedding []float32, topK int) ([]domain.ConversationTurn, error)
	ClearSession(ctx context.Context, sessionID string) error
}

// SemanticCacheRepository caches LLM responses by embedding similarity.
type SemanticCacheRepository interface {
	FindSimilar(ctx context.Context, embedding []float32, threshold float64) (string, bool, error)
	Store(ctx context.Context, embedding []float32, response string, ttl time.Duration) error
}

// TTSClient synthesizes text into audio.
type TTSClient interface {
	Synthesize(ctx context.Context, text string, lang string) (audioBytes []byte, format string, err error)
	HealthCheck(ctx context.Context) error
}

// HUDBroadcaster sends responses to connected WebSocket clients.
type HUDBroadcaster interface {
	BroadcastText(ctx context.Context, response domain.VBotResponse) error
	BroadcastTextChunk(ctx context.Context, chunk string) error
	BroadcastAudio(ctx context.Context, audioBytes []byte, format string) error
	BroadcastAvatarCommand(ctx context.Context, cmd domain.AvatarCommand) error
}
