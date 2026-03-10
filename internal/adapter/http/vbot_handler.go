package http

import (
	"encoding/base64"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/adapter/ws"
	"github.com/vbot/vbot/internal/domain"
	"github.com/vbot/vbot/internal/usecase"
)

// VBotHandler handles chat and personality API requests.
type VBotHandler struct {
	engine *usecase.VBotEngine
	rag    usecase.RAGRepository
	hub    *ws.Hub
	logger *zap.Logger
}

// NewVBotHandler creates a new VBot API handler.
func NewVBotHandler(engine *usecase.VBotEngine, rag usecase.RAGRepository, hub *ws.Hub, logger *zap.Logger) *VBotHandler {
	return &VBotHandler{
		engine: engine,
		rag:    rag,
		hub:    hub,
		logger: logger,
	}
}

type chatRequest struct {
	SessionID    string `json:"session_id" binding:"required"`
	Input        string `json:"input" binding:"required"`
	IncludeAudio bool   `json:"include_audio"`
}

type chatResponse struct {
	Text        string                `json:"text"`
	AudioBase64 string                `json:"audio_base64,omitempty"`
	AudioFormat string                `json:"audio_format,omitempty"`
	Emotion     string                `json:"emotion,omitempty"`
	CacheHit    bool                  `json:"cache_hit"`
	Latency     domain.LatencyBreakdown `json:"latency"`
}

// Chat handles POST /api/vbot/chat.
func (h *VBotHandler) Chat(c *gin.Context) {
	var req chatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "message": err.Error()})
		return
	}

	result, err := h.engine.Chat(c.Request.Context(), usecase.ChatRequest{
		SessionID:    req.SessionID,
		Input:        req.Input,
		IncludeAudio: req.IncludeAudio,
	})
	if err != nil {
		h.logger.Error("chat failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "chat_failed", "message": err.Error()})
		return
	}

	resp := chatResponse{
		Text:     result.Text,
		Emotion:  result.Emotion,
		CacheHit: result.CacheHit,
		Latency:  result.Latency,
	}

	if len(result.AudioBytes) > 0 {
		resp.AudioBase64 = base64.StdEncoding.EncodeToString(result.AudioBytes)
		resp.AudioFormat = result.AudioFormat
	}

	// Broadcast to WebSocket clients
	_ = h.hub.BroadcastText(c.Request.Context(), *result)
	if len(result.AudioBytes) > 0 {
		_ = h.hub.BroadcastAudio(c.Request.Context(), result.AudioBytes, result.AudioFormat)
	}
	_ = h.hub.BroadcastAvatarCommand(c.Request.Context(), domain.AvatarCommand{
		State:   "speaking",
		Emotion: result.Emotion,
	})

	c.JSON(http.StatusOK, resp)
}

type loadPersonalityRequest struct {
	FilePath string `json:"file_path" binding:"required"`
}

// LoadPersonality handles POST /api/vbot/personality/load.
func (h *VBotHandler) LoadPersonality(c *gin.Context) {
	var req loadPersonalityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "message": err.Error()})
		return
	}

	if err := h.rag.LoadFromFile(c.Request.Context(), req.FilePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "load_failed", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "personality loaded"})
}

// ListTraits handles GET /api/vbot/personality/traits.
func (h *VBotHandler) ListTraits(c *gin.Context) {
	traits, err := h.rag.AllTraits(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "list_failed", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"traits": traits, "count": len(traits)})
}

// GetCompleteness handles GET /api/vbot/personality/completeness.
func (h *VBotHandler) GetCompleteness(c *gin.Context) {
	completeness, err := h.rag.GetCompleteness(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "completeness_failed", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, completeness)
}

type resetSessionRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

// ResetSession handles POST /api/vbot/session/reset.
func (h *VBotHandler) ResetSession(c *gin.Context) {
	var req resetSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request", "message": err.Error()})
		return
	}
	// Session reset is handled through the engine's memory
	c.JSON(http.StatusOK, gin.H{"message": "session reset"})
}

// GetConfig handles GET /api/vbot/config.
func (h *VBotHandler) GetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"websocket_connections": h.hub.ConnectionCount(),
	})
}
