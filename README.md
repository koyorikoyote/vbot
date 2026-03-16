# VBot Implementation

## Description

A Go project (`vbot/`) implementing a VTuber AI personality bot.

## Project Structure

```
vbot/
├── cmd/vbot/main.go            # Entry point, wires all dependencies
├── internal/
│   ├── domain/                  # Personality, Transcript, Error types
│   ├── usecase/                 # Interfaces, VBotEngine, IngestEngine, SemanticCache
│   └── adapter/
│       ├── llm/                 # Ollama chat + embedding clients
│       ├── stt/                 # ffmpeg processor + whisper.cpp client
│       ├── rag/                 # Redis VSS RAG, trait extractor, defaults
│       ├── memory/              # Redis conversation memory
│       ├── cache/               # Redis semantic cache
│       ├── tts/                 # Piper + Windows SAPI + fallback chain
│       ├── http/                # Gin router, chat/ingest handlers
│       └── ws/                  # WebSocket hub + client
├── pkg/                         # config, logger, sanitize
├── web/simulator/               # HTML/CSS/JS avatar simulator
├── data/personalities/          # immergold.yaml defaults
├── docker-compose.yaml          # Redis, Piper, Whisper sidecars
└── .env.example                 # All environment variables
```

## Key Features Implemented

RAG Personality:
(redis_rag.go, defaults.go)
Redis VSS with personality traits

Completeness %:
(redis_rag.go → GetCompleteness)
Per-category coverage with recommendations

Stream Ingestion:
(ingest_engine.go, ffmpeg.go, whisper_client.go)
ffmpeg → chunked whisper → LLM trait extraction → Redis

TTS Fallback:
(tts_chain.go, sapi_client.go)
Piper → Windows SAPI automatic failover

Chat Pipeline:
(vbot_engine.go)
Embed → Cache check → RAG → LLM → TTS → Broadcast

Semantic Cache:
(redis_cache.go, semantic_cache.go)
Redis VSS cosine similarity caching

Placeholder Avatar:
(app.js)
Canvas avatar with Web Audio API mouth sync + visualizer ring

WebSocket:
(hub.go)
Text, audio binary, and avatar command broadcasting

## Verification

- `go build ./...` — compiles with zero errors
- `go vet ./...` — passes with zero warnings

## Install Whisper Model

curl.exe -L -o data/whisper-models/ggml-medium.bin https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.bin

## How to Run

# 1. Start dependencies

cd vbot
docker compose up -d

# 2. Pull LLM + embedding models

ollama pull llama3.1:8b
ollama pull nomic-embed-text

# 3. Copy env

cp .env.example .env

# 4. Run

go build -o vbot.exe ./cmd/vbot; ./vbot.exe

# 5. Open simulator

http://localhost:8090/simulator/

## API Endpoints

| Method | Path                                 | Purpose               |
| ------ | ------------------------------------ | --------------------- |
| POST   | `/api/vbot/chat`                     | Send chat message     |
| GET    | `/api/vbot/personality/completeness` | RAG coverage %        |
| GET    | `/api/vbot/personality/traits`       | List all traits       |
| POST   | `/api/vbot/personality/load`         | Load YAML personality |
| POST   | `/api/vbot/ingest/upload`            | Upload stream file    |
| GET    | `/api/vbot/ingest/jobs/:id`          | Track ingestion       |
| GET    | `/ws`                                | WebSocket endpoint    |
| GET    | `/health`                            | Health check          |
| GET    | `/metrics`                           | Prometheus metrics    |
