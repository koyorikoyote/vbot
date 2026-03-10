package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/domain"
	"github.com/vbot/vbot/internal/usecase"
)

// LLMTraitExtractor implements usecase.TraitExtractor using an LLM.
type LLMTraitExtractor struct {
	llm    usecase.LLMClient
	logger *zap.Logger
}

// NewLLMTraitExtractor creates a new trait extractor.
func NewLLMTraitExtractor(llm usecase.LLMClient, logger *zap.Logger) *LLMTraitExtractor {
	return &LLMTraitExtractor{
		llm:    llm,
		logger: logger,
	}
}

const traitExtractionPrompt = `You are a personality analyst. Extract personality traits from this VTuber stream transcript.

For each trait, output a JSON object with:
- "category": one of ["speech_pattern", "attitude", "catchphrase", "reaction_pattern", "topic_preference", "humor_style", "interaction_style", "backstory_hint", "emotional_range"]
- "content": a concise description of the trait (1-3 sentences)
- "evidence": the exact quote from the transcript that demonstrates this trait

Output ONLY a valid JSON array. No explanation, no markdown.

Transcript (%s - %s):
%s`

// ExtractTraits sends a transcript section to the LLM and parses extracted traits.
func (e *LLMTraitExtractor) ExtractTraits(ctx context.Context, transcript string, sourceFile string, startTime, endTime float64) ([]domain.PersonalityTrait, error) {
	startTS := formatTimestamp(startTime)
	endTS := formatTimestamp(endTime)

	prompt := fmt.Sprintf(traitExtractionPrompt, startTS, endTS, transcript)

	messages := []domain.ConversationTurn{
		{Role: "user", Content: prompt},
	}

	response, err := e.llm.Chat(ctx, messages, "You are a personality analysis assistant. Always respond with valid JSON arrays only.")
	if err != nil {
		return nil, fmt.Errorf("llm trait extraction: %w", err)
	}

	// Clean response — strip markdown code fences if present
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var rawTraits []struct {
		Category string `json:"category"`
		Content  string `json:"content"`
		Evidence string `json:"evidence"`
	}

	if err := json.Unmarshal([]byte(response), &rawTraits); err != nil {
		e.logger.Warn("failed to parse trait extraction response", zap.Error(err), zap.String("response", response))
		return nil, fmt.Errorf("%w: parse traits: %v", domain.ErrLLMResponseInvalid, err)
	}

	traits := make([]domain.PersonalityTrait, 0, len(rawTraits))
	for i, rt := range rawTraits {
		if rt.Category == "" || rt.Content == "" {
			continue
		}

		traits = append(traits, domain.PersonalityTrait{
			TraitID:        fmt.Sprintf("stream_%s_%d_%.0f", rt.Category, i, startTime),
			Category:       rt.Category,
			Content:        rt.Content,
			Evidence:       rt.Evidence,
			SourceFile:     sourceFile,
			TimestampRange: fmt.Sprintf("%s-%s", startTS, endTS),
		})
	}

	e.logger.Info("traits extracted", zap.Int("count", len(traits)), zap.String("range", fmt.Sprintf("%s-%s", startTS, endTS)))
	return traits, nil
}

func formatTimestamp(seconds float64) string {
	h := int(seconds) / 3600
	m := (int(seconds) % 3600) / 60
	s := int(seconds) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}
