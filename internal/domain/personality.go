package domain

import "time"

// PersonalityTrait represents a single personality characteristic.
type PersonalityTrait struct {
	TraitID        string    `json:"trait_id"`
	Category       string    `json:"category"`
	Content        string    `json:"content"`
	Evidence       string    `json:"evidence,omitempty"`
	SourceFile     string    `json:"source_file,omitempty"`
	TimestampRange string    `json:"timestamp_range,omitempty"`
	Embedding      []float32 `json:"embedding,omitempty"`
}

// ConversationTurn is a single message in a conversation.
type ConversationTurn struct {
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// VBotResponse is the output of the chat engine.
type VBotResponse struct {
	Text        string           `json:"text"`
	AudioBytes  []byte           `json:"audio_bytes,omitempty"`
	AudioFormat string           `json:"audio_format,omitempty"`
	Emotion     string           `json:"emotion,omitempty"`
	CacheHit    bool             `json:"cache_hit"`
	Latency     LatencyBreakdown `json:"latency"`
}

// LatencyBreakdown records timing for each pipeline stage.
type LatencyBreakdown struct {
	RAGRetrievalMs int64 `json:"rag_retrieval_ms"`
	CacheCheckMs   int64 `json:"cache_check_ms"`
	LLMInferenceMs int64 `json:"llm_inference_ms"`
	TTSSynthesisMs int64 `json:"tts_synthesis_ms"`
	TotalMs        int64 `json:"total_ms"`
}

// RAGCompleteness tracks personality coverage per category.
type RAGCompleteness struct {
	OverallPercent int                          `json:"overall_completeness"`
	Categories     map[string]CategoryStatus    `json:"categories"`
	Recommendation string                       `json:"recommendation"`
}

// CategoryStatus describes trait coverage for one personality category.
type CategoryStatus struct {
	TraitCount   int    `json:"trait_count"`
	Target       int    `json:"target"`
	Completeness int    `json:"completeness"`
	Source       string `json:"source"` // "default", "uploaded", "mixed"
	Message      string `json:"message,omitempty"`
}

// PersonalityCategory constants.
const (
	CategorSpeechPattern    = "speech_pattern"
	CategoryAttitude        = "attitude"
	CategoryCatchphrase     = "catchphrase"
	CategoryReactionPattern = "reaction_pattern"
	CategoryTopicPreference = "topic_preference"
	CategoryHumorStyle      = "humor_style"
	CategoryInteractionStyle = "interaction_style"
	CategoryBackstoryHint   = "backstory_hint"
	CategoryEmotionalRange  = "emotional_range"
)

// CategoryTargets defines the ideal trait count per category.
var CategoryTargets = map[string]int{
	CategorSpeechPattern:     5,
	CategoryAttitude:         5,
	CategoryCatchphrase:      10,
	CategoryReactionPattern:  5,
	CategoryTopicPreference:  8,
	CategoryHumorStyle:       5,
	CategoryInteractionStyle: 5,
	CategoryBackstoryHint:    5,
	CategoryEmotionalRange:   5,
}

// AvatarCommand is sent over WebSocket to drive avatar state.
type AvatarCommand struct {
	State   string `json:"state"`   // "speaking", "idle", "reacting"
	Emotion string `json:"emotion"` // "sarcastic", "smug", "annoyed", "neutral"
}
