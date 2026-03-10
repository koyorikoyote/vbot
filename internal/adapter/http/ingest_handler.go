package http

import (
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/usecase"
)

// IngestHandler handles stream ingestion API requests.
type IngestHandler struct {
	engine       *usecase.IngestEngine
	uploadDir    string
	maxFileSizeMB int
	logger       *zap.Logger
}

// NewIngestHandler creates a new ingestion handler.
func NewIngestHandler(engine *usecase.IngestEngine, uploadDir string, maxFileSizeMB int, logger *zap.Logger) *IngestHandler {
	return &IngestHandler{
		engine:        engine,
		uploadDir:     uploadDir,
		maxFileSizeMB: maxFileSizeMB,
		logger:        logger,
	}
}

// Upload handles POST /api/vbot/ingest/upload.
func (h *IngestHandler) Upload(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(h.maxFileSizeMB)*1024*1024)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file_required", "message": err.Error()})
		return
	}
	defer file.Close()

	// Generate unique filename
	ext := filepath.Ext(header.Filename)
	savedName := fmt.Sprintf("ingest_%d%s", time.Now().UnixNano(), ext)
	savedPath := filepath.Join(h.uploadDir, savedName)

	if err := c.SaveUploadedFile(header, savedPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "save_failed", "message": err.Error()})
		return
	}

	jobID := fmt.Sprintf("ingest-%d", time.Now().UnixNano())
	h.engine.IngestStream(c.Request.Context(), jobID, savedPath)

	c.JSON(http.StatusAccepted, gin.H{
		"job_id":      jobID,
		"status":      "pending",
		"source_file": header.Filename,
		"message":     fmt.Sprintf("Ingestion started. Track progress at GET /api/vbot/ingest/jobs/%s", jobID),
	})
}

// ListJobs handles GET /api/vbot/ingest/jobs.
func (h *IngestHandler) ListJobs(c *gin.Context) {
	jobs := h.engine.ListJobs()
	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// GetJob handles GET /api/vbot/ingest/jobs/:id.
func (h *IngestHandler) GetJob(c *gin.Context) {
	jobID := c.Param("id")
	job, err := h.engine.GetJob(jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, job)
}
