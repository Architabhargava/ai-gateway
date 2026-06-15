# AI API Gateway

A production-grade AI governance system built in Go. Acts as a reverse proxy 
between your applications and AI providers (Groq / Llama 3), enforcing security 
policies, logging every interaction, and exposing a live audit dashboard.

## What it does

Every AI request passes through a multi-layer pipeline before reaching the model:

1. **Authentication** — validates API keys via the X-API-Key header
2. **Policy engine** — blocks harmful prompts and rate limits users (5 req/min)
3. **Audit logger** — saves every request with timestamp, IP, prompt, and response
4. **AI proxy** — forwards approved requests to Groq (Llama 3.3 70B, free tier)
5. **Dashboard** — live web UI showing stats, charts, and searchable logs

## Features

- Chat UI at `/` — send prompts and see AI responses directly in the browser
- REST API at `/ai` — JSON endpoint for programmatic access
- Audit dashboard at `/dashboard` — real-time logs with auto-refresh
- Blocked word detection — flags jailbreak attempts and policy violations
- Rate limiting — per-IP request throttling with configurable windows
- Multi-key support — issue separate API keys per user or application
- SQLite storage — zero-config persistent logging locally
- Docker ready — single Dockerfile for containerised deployment

## Tech stack

| Layer | Technology |
|---|---|
| Language | Go 1.26 |
| AI provider | Groq API — Llama 3.3 70B (free tier) |
| Storage | SQLite (local) / in-memory (cloud) |
| Deployment | Render / Docker |
| Auth | API key via request header |

## Getting started

1. Clone the repo
2. Add your Groq API key to `.env`
3. Run `go run cmd/main.go`
4. Open `http://localhost:8080`

## Environment variables

| Variable | Description |
|---|---|
| `GROQ_API_KEY` | Your Groq API key (get one free at console.groq.com) |
| `GATEWAY_API_KEYS` | Comma-separated list of valid client API keys |

## Project structure
ai-gateway/

├── cmd/main.go               # Entry point

├── internal/

│   ├── auth/auth.go          # API key validation

│   ├── policy/policy.go      # Blocked words + rate limiting

│   ├── logger/logger.go      # SQLite + in-memory audit log

│   ├── gateway/gateway.go    # Core request handler

│   ├── gateway/groq.go       # Groq AI integration

│   └── dashboard/dashboard.go # Chat UI + audit dashboard

## API usage

Send a prompt through the gateway:

```bash
curl -X POST https://your-app.onrender.com/ai \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-key-here" \
  -d '{"prompt": "explain what is an API"}'
```

Response:
```json
{
  "status": "success",
  "prompt": "explain what is an API",
  "response": "An API is..."
}
```
