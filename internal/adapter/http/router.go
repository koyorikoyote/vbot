package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/adapter/ws"
	"github.com/vbot/vbot/internal/usecase"
)

// Router sets up the Gin HTTP router.
type Router struct {
	engine       *gin.Engine
	vbotHandler  *VBotHandler
	ingestHandler *IngestHandler
	hub          *ws.Hub
	logger       *zap.Logger
}

// NewRouter creates and configures the HTTP router.
func NewRouter(
	vbotEngine *usecase.VBotEngine,
	ingestEngine *usecase.IngestEngine,
	rag usecase.RAGRepository,
	hub *ws.Hub,
	uploadDir string,
	maxFileSizeMB int,
	logger *zap.Logger,
) *Router {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())

	vbotHandler := NewVBotHandler(vbotEngine, rag, hub, logger)
	ingestHandler := NewIngestHandler(ingestEngine, uploadDir, maxFileSizeMB, logger)

	r := &Router{
		engine:        engine,
		vbotHandler:   vbotHandler,
		ingestHandler: ingestHandler,
		hub:           hub,
		logger:        logger,
	}

	r.setupRoutes()
	return r
}

func (r *Router) setupRoutes() {
	// Health
	r.engine.GET("/health", r.healthHandler)
	r.engine.GET("/ready", r.readyHandler)
	r.engine.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// API
	api := r.engine.Group("/api/vbot")
	{
		api.POST("/chat", r.vbotHandler.Chat)
		api.POST("/personality/load", r.vbotHandler.LoadPersonality)
		api.GET("/personality/traits", r.vbotHandler.ListTraits)
		api.GET("/personality/completeness", r.vbotHandler.GetCompleteness)
		api.POST("/session/reset", r.vbotHandler.ResetSession)
		api.GET("/config", r.vbotHandler.GetConfig)

		api.POST("/ingest/upload", r.ingestHandler.Upload)
		api.GET("/ingest/jobs", r.ingestHandler.ListJobs)
		api.GET("/ingest/jobs/:id", r.ingestHandler.GetJob)
	}

	// WebSocket
	r.engine.GET("/ws", r.wsHandler)

	// Static files for simulator
	r.engine.Static("/simulator", "./web/simulator")
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (r *Router) wsHandler(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		r.logger.Error("ws upgrade failed", zap.Error(err))
		return
	}

	client := ws.NewClient(r.hub, conn)
	r.hub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}

func (r *Router) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (r *Router) readyHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

// Handler returns the HTTP handler.
func (r *Router) Handler() http.Handler {
	return r.engine
}
