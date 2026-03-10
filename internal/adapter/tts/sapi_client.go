package tts

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/domain"
)

// SAPIClient implements usecase.TTSClient using Windows SAPI via PowerShell.
type SAPIClient struct {
	tempDir string
	logger  *zap.Logger
}

// NewSAPIClient creates a new Windows SAPI TTS client.
func NewSAPIClient(tempDir string, logger *zap.Logger) *SAPIClient {
	return &SAPIClient{
		tempDir: tempDir,
		logger:  logger,
	}
}

// Synthesize converts text to WAV audio using Windows SAPI.
func (c *SAPIClient) Synthesize(ctx context.Context, text string) ([]byte, string, error) {
	outputPath := filepath.Join(c.tempDir, fmt.Sprintf("sapi_%d.wav", os.Getpid()))

	// Use SAPI.SpVoice COM object — works in both PowerShell 5 and 7
	script := fmt.Sprintf(`
$sp = New-Object -ComObject SAPI.SpVoice
$voices = $sp.GetVoices()
$femaleVoice = $null
for ($i = 0; $i -lt $voices.Count; $i++) {
    $voice = $voices.Item($i)
    $desc = $voice.GetDescription()
    if ($desc -match 'Female' -or $desc -match 'Zira' -or $desc -match 'Haruka' -or $desc -match 'Sayaka') {
        $femaleVoice = $voice
        break
    }
}
if ($femaleVoice) { $sp.Voice = $femaleVoice }
$stream = New-Object -ComObject SAPI.SpFileStream
$format = New-Object -ComObject SAPI.SpAudioFormat
$format.Type = 22
$stream.Format = $format
$stream.Open('%s', 3)
$sp.AudioOutputStream = $stream
$sp.Speak('%s')
$stream.Close()
`, outputPath, escapePowerShell(text))

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("%w: sapi: %v: %s", domain.ErrTTSUnavailable, err, string(output))
	}

	audioBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, "", fmt.Errorf("read sapi output: %w", err)
	}

	// Clean up temp file
	_ = os.Remove(outputPath)

	c.logger.Debug("sapi synthesis completed", zap.Int("text_len", len(text)), zap.Int("audio_bytes", len(audioBytes)))
	return audioBytes, "wav", nil
}

// HealthCheck verifies PowerShell and SAPI availability.
func (c *SAPIClient) HealthCheck(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		"$sp = New-Object -ComObject SAPI.SpVoice; $sp.GetVoices().Count")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: sapi health check: %v: %s", domain.ErrTTSUnavailable, err, string(output))
	}
	return nil
}

func escapePowerShell(s string) string {
	// Escape single quotes for PowerShell strings
	result := ""
	for _, c := range s {
		if c == '\'' {
			result += "''"
		} else {
			result += string(c)
		}
	}
	return result
}
