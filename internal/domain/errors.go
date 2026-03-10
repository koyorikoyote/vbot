package domain

import "errors"

var (
	ErrLLMUnavailable      = errors.New("llm service unavailable")
	ErrLLMTimeout          = errors.New("llm inference timed out")
	ErrLLMResponseInvalid  = errors.New("llm response invalid")
	ErrTTSUnavailable      = errors.New("tts service unavailable")
	ErrSTTUnavailable      = errors.New("stt service unavailable")
	ErrFFmpegFailed        = errors.New("ffmpeg processing failed")
	ErrPersonalityNotFound = errors.New("personality file not found")
	ErrSessionNotFound     = errors.New("session not found")
	ErrSanitizationDiscard = errors.New("content discarded by sanitization")
	ErrIngestionFailed     = errors.New("stream ingestion failed")
	ErrJobNotFound         = errors.New("ingestion job not found")
	ErrFileTooLarge        = errors.New("uploaded file exceeds size limit")
	ErrUnsupportedFormat   = errors.New("unsupported media format")
)
