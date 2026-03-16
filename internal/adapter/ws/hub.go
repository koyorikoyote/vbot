package ws

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/vbot/vbot/internal/domain"
)

// Hub maintains the set of active WebSocket clients and broadcasts messages.
type Hub struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
	logger     *zap.Logger
}

// NewHub creates a new WebSocket hub.
func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		logger:     logger,
	}
}

// Run starts the hub's event loop.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			h.logger.Info("ws client connected", zap.Int("total", len(h.clients)))
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			h.logger.Info("ws client disconnected", zap.Int("total", len(h.clients)))
		}
	}
}

// BroadcastText sends a text response to all connected clients.
func (h *Hub) BroadcastText(_ context.Context, response domain.VBotResponse) error {
	msg := wsMessage{
		Type:    "text",
		Payload: response,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		select {
		case client.send <- data:
		default:
			// Client buffer full — skip
		}
	}
	return nil
}

// BroadcastTextChunk sends a raw string chunk to all connected clients.
func (h *Hub) BroadcastTextChunk(_ context.Context, chunk string) error {
	msg := wsMessage{
		Type:    "text_chunk",
		Payload: chunk,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		select {
		case client.send <- data:
		default:
		}
	}
	return nil
}

// BroadcastAudio sends binary audio data to all connected clients.
func (h *Hub) BroadcastAudio(_ context.Context, audioBytes []byte, format string) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		select {
		case client.sendBinary <- audioBytes:
		default:
		}
	}
	return nil
}

// BroadcastAvatarCommand sends an avatar command to all clients.
func (h *Hub) BroadcastAvatarCommand(_ context.Context, cmd domain.AvatarCommand) error {
	msg := wsMessage{
		Type:    "avatar_command",
		Payload: cmd,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		select {
		case client.send <- data:
		default:
		}
	}
	return nil
}

// Register adds a client to the hub.
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// ConnectionCount returns the number of active connections.
func (h *Hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

type wsMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// Client represents a single WebSocket connection.
type Client struct {
	hub        *Hub
	conn       *websocket.Conn
	send       chan []byte
	sendBinary chan []byte
}

// NewClient creates a new WebSocket client.
func NewClient(hub *Hub, conn *websocket.Conn) *Client {
	return &Client{
		hub:        hub,
		conn:       conn,
		send:       make(chan []byte, 256),
		sendBinary: make(chan []byte, 16),
	}
}

// WritePump sends messages from the hub to the WebSocket connection.
func (c *Client) WritePump() {
	defer func() {
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case audio, ok := <-c.sendBinary:
			if !ok {
				return
			}
			if err := c.conn.WriteMessage(websocket.BinaryMessage, audio); err != nil {
				return
			}
		}
	}
}

// ReadPump reads messages from the WebSocket connection.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		// Messages from clients are handled by the HTTP handler, not here
	}
}
