package tts

import (
	"context"
	"encoding/base64"
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
// lang selects the voice: "ja" picks Japanese voices (Haruka/Sayaka),
// anything else picks English female voices (Zira).
func (c *SAPIClient) Synthesize(ctx context.Context, text string, lang string) ([]byte, string, error) {
	outputPath := filepath.Join(c.tempDir, fmt.Sprintf("sapi_%d.wav", os.Getpid()))

	// Build voice match pattern based on language
	voicePattern := "'Female' -or $desc -match 'Zira'"
	if lang == "ja" {
		voicePattern = "'Haruka' -or $desc -match 'Sayaka' -or $desc -match 'Nanami' -or $desc -match 'Ayumi'"
	}

	b64Text := base64.StdEncoding.EncodeToString([]byte(text))

	script := fmt.Sprintf(`
$sp = New-Object -ComObject SAPI.SpVoice
$voices = $sp.GetVoices()
$targetVoice = $null
for ($i = 0; $i -lt $voices.Count; $i++) {
    $voice = $voices.Item($i)
    $desc = $voice.GetDescription()
    if ($desc -match %s) {
        $targetVoice = $voice
        break
    }
}
if ($targetVoice) { $sp.Voice = $targetVoice }

$textBytes = [System.Convert]::FromBase64String('%s')
$speakText = [System.Text.Encoding]::UTF8.GetString($textBytes)

$stream = New-Object -ComObject SAPI.SpFileStream
$format = New-Object -ComObject SAPI.SpAudioFormat
$format.Type = 22
$stream.Format = $format
$stream.Open('%s', 3)
$sp.AudioOutputStream = $stream
$sp.Speak($speakText)
$stream.Close()
`, voicePattern, b64Text, outputPath)

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("%w: sapi: %v: %s", domain.ErrTTSUnavailable, err, string(output))
	}

	audioBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, "", fmt.Errorf("read sapi output: %w", err)
	}

	_ = os.Remove(outputPath)

	c.logger.Debug("sapi synthesis completed", zap.String("lang", lang), zap.Int("text_len", len(text)), zap.Int("audio_bytes", len(audioBytes)))
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


