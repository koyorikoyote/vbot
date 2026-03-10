package stt

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/domain"
)

// FFmpegProcessor implements usecase.AudioProcessor via ffmpeg CLI.
type FFmpegProcessor struct {
	ffmpegPath  string
	ffprobePath string
	tempDir     string
	logger      *zap.Logger
}

// NewFFmpegProcessor creates a new audio processor.
func NewFFmpegProcessor(ffmpegPath, ffprobePath, tempDir string, logger *zap.Logger) *FFmpegProcessor {
	return &FFmpegProcessor{
		ffmpegPath:  ffmpegPath,
		ffprobePath: ffprobePath,
		tempDir:     tempDir,
		logger:      logger,
	}
}

// ExtractAudio extracts mono 16kHz WAV audio from a video/audio file.
func (p *FFmpegProcessor) ExtractAudio(ctx context.Context, inputPath, outputPath string) error {
	args := []string{
		"-i", inputPath,
		"-vn",               // no video
		"-acodec", "pcm_s16le", // 16-bit PCM
		"-ar", "16000",      // 16kHz sample rate
		"-ac", "1",          // mono
		"-y",                // overwrite
		outputPath,
	}

	cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %v: %s", domain.ErrFFmpegFailed, err, string(output))
	}

	p.logger.Info("audio extracted", zap.String("input", inputPath), zap.String("output", outputPath))
	return nil
}

// SplitChunks splits an audio file into chunks of the given duration with overlap.
func (p *FFmpegProcessor) SplitChunks(ctx context.Context, audioPath string, chunkDuration, overlapDuration time.Duration) ([]string, error) {
	totalDuration, err := p.GetDuration(ctx, audioPath)
	if err != nil {
		return nil, fmt.Errorf("get duration: %w", err)
	}

	chunkSec := int(chunkDuration.Seconds())
	overlapSec := int(overlapDuration.Seconds())
	stepSec := chunkSec - overlapSec
	if stepSec <= 0 {
		stepSec = chunkSec
	}

	totalSec := int(totalDuration.Seconds())
	baseName := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))

	var chunkPaths []string
	chunkIdx := 0

	for startSec := 0; startSec < totalSec; startSec += stepSec {
		endSec := startSec + chunkSec
		if endSec > totalSec {
			endSec = totalSec
		}

		chunkFile := filepath.Join(p.tempDir, fmt.Sprintf("%s_chunk_%03d.wav", baseName, chunkIdx))

		args := []string{
			"-i", audioPath,
			"-ss", strconv.Itoa(startSec),
			"-t", strconv.Itoa(endSec - startSec),
			"-acodec", "pcm_s16le",
			"-ar", "16000",
			"-ac", "1",
			"-y",
			chunkFile,
		}

		cmd := exec.CommandContext(ctx, p.ffmpegPath, args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("%w: chunk %d: %v: %s", domain.ErrFFmpegFailed, chunkIdx, err, string(output))
		}

		chunkPaths = append(chunkPaths, chunkFile)
		chunkIdx++

		p.logger.Debug("chunk created", zap.Int("index", chunkIdx-1), zap.Int("start_sec", startSec), zap.Int("end_sec", endSec))
	}

	p.logger.Info("audio split into chunks", zap.Int("total_chunks", len(chunkPaths)))
	return chunkPaths, nil
}

// GetDuration returns the duration of an audio/video file.
func (p *FFmpegProcessor) GetDuration(ctx context.Context, filePath string) (time.Duration, error) {
	args := []string{
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "csv=p=0",
		filePath,
	}

	cmd := exec.CommandContext(ctx, p.ffprobePath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("%w: ffprobe: %v: %s", domain.ErrFFmpegFailed, err, string(output))
	}

	durationStr := strings.TrimSpace(string(output))
	if durationStr == "" {
		return 0, fmt.Errorf("%w: empty duration output", domain.ErrFFmpegFailed)
	}

	durationSec, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: parse duration %q: %v", domain.ErrFFmpegFailed, durationStr, err)
	}

	return time.Duration(durationSec * float64(time.Second)), nil
}

// Cleanup removes temporary chunk files.
func (p *FFmpegProcessor) Cleanup(chunkPaths []string) {
	for _, path := range chunkPaths {
		if err := os.Remove(path); err != nil {
			p.logger.Warn("failed to remove chunk file", zap.String("path", path), zap.Error(err))
		}
	}
}
