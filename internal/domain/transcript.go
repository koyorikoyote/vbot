package domain

// TranscriptSegment is a single transcribed text segment with timestamps.
type TranscriptSegment struct {
	Text      string  `json:"text"`
	StartTime float64 `json:"start"` // seconds from start of original file
	EndTime   float64 `json:"end"`
}

// TranscriptChunk represents a transcribed audio chunk.
type TranscriptChunk struct {
	ChunkIndex int                 `json:"chunk_index"`
	FilePath   string              `json:"file_path"`
	Segments   []TranscriptSegment `json:"segments"`
}

// IngestionJob tracks the status of a stream-to-RAG ingestion.
type IngestionJob struct {
	JobID       string `json:"job_id"`
	SourceFile  string `json:"source_file"`
	Status      string `json:"status"` // pending, extracting, transcribing, extracting_traits, storing, done, error
	Progress    int    `json:"progress"`
	TotalChunks int    `json:"total_chunks"`
	DoneChunks  int    `json:"done_chunks"`
	TraitsFound int    `json:"traits_found"`
	Error       string `json:"error,omitempty"`
}

// IngestionStatus constants.
const (
	StatusPending          = "pending"
	StatusExtractingAudio  = "extracting"
	StatusTranscribing     = "transcribing"
	StatusExtractingTraits = "extracting_traits"
	StatusStoring          = "storing"
	StatusDone             = "done"
	StatusError            = "error"
)
