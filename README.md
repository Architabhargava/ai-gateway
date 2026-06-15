# AI API Gateway

A production-grade AI governance layer built in Go. Sits as a reverse proxy between your applications and any LLM provider — enforcing authentication, policy rules, rate limits, and maintaining a tamper-evident audit trail of every AI interaction. Ships with a browser-based chat interface and a live audit dashboard, all in a single binary with zero external dependencies beyond a SQLite file.

> Built for teams and developers who need visibility, control, and accountability over how AI is being used — without the overhead of a managed service.

---

## Why this exists

Most applications that integrate LLMs call the provider API directly. This works fine until you need to answer questions like:

- Who sent that prompt that caused the incident last Tuesday?
- Why did our API bill spike? Which user or service made 4,000 calls?
- How do we prevent employees from leaking confidential data into a public LLM?
- Can we enforce a consistent system policy across every AI call in our codebase?

The AI Gateway solves all of these by becoming the single choke point that every AI call must pass through. Your apps never talk to Groq or OpenAI directly — they talk to your gateway, which decides what gets through, logs everything, and shows you a live picture of all AI activity.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        Client layer                          │
│   Browser Chat UI (/),  REST API (/ai),  Any HTTP client    │
└───────────────────────────┬─────────────────────────────────┘
                            │ HTTP POST + X-API-Key header
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                    Go Gateway Server                         │
│                    cmd/main.go · :8080                       │
│                                                              │
│  ┌─────────────┐   ┌─────────────┐   ┌──────────────────┐  │
│  │  Auth layer  │──▶│Policy engine│──▶│  Audit logger    │  │
│  │  auth.go     │   │ policy.go   │   │  logger.go       │  │
│  │              │   │             │   │                  │  │
│  │ Validates    │   │ Blocked word│   │ Writes every     │  │
│  │ X-API-Key    │   │ detection + │   │ request to       │  │
│  │ header       │   │ rate limit  │   │ SQLite / memory  │  │
│  └─────────────┘   └─────────────┘   └──────────────────┘  │
│                                                 │             │
│                                                ▼             │
│                                     ┌──────────────────┐    │
│                                     │   Groq proxy     │    │
│                                     │   groq.go        │    │
│                                     │                  │    │
│                                     │ Forwards to      │    │
│                                     │ Llama 3.3 70B    │    │
│                                     └──────────────────┘    │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│                     Observability layer                      │
│   /dashboard — live stats, hourly chart, searchable logs    │
└─────────────────────────────────────────────────────────────┘
```

Every inbound request traverses the layers left to right. A failure at any layer short-circuits the pipeline — the request never reaches the AI provider, and the block is logged. Only requests that clear all layers get proxied upstream.

---

## Request lifecycle in detail

### 1. HTTP listener — `cmd/main.go`

The Go standard library's `net/http` server binds to `:8080` and routes traffic via a `ServeMux`:

| Route | Handler | Purpose |
|---|---|---|
| `GET /` | `HandleHome` | Serves the browser chat UI |
| `POST /ai` | `HandleAI` | Main gateway endpoint — all AI calls |
| `GET /dashboard` | `HandleDashboard` | Live audit dashboard |
| `GET /health` | `handleHealth` | Liveness check for deployment platforms |

In production on Render, the server reads the `PORT` environment variable set by the platform and binds to that instead of hardcoding 8080.

```go
port := os.Getenv("PORT")
if port == "" {
    port = "8080"
}
http.ListenAndServe(":"+port, mux)
```

The `FLY_APP_NAME` and `RENDER` environment variables are used to switch the SQLite path to a persistent volume mount (`/data/gateway.db`) when running in the cloud, versus a local file when running on your machine.

---

### 2. Authentication — `internal/auth/auth.go`

Before any business logic runs, the gateway validates the caller's identity via the `X-API-Key` HTTP header. Keys are loaded at startup from the `GATEWAY_API_KEYS` environment variable as a comma-separated list and stored in a `map[string]bool` for O(1) lookup.

```
Request headers:
  Content-Type: application/json
  X-API-Key: key-alpha-123        ← required
```

**What happens on failure:** The request is rejected immediately with `401 Unauthorized` and a JSON body explaining the reason. The rejection is also logged so you can see patterns of invalid access attempts in the dashboard.

**Why multiple keys:** Issuing a separate key per application or team member means you can revoke a single key without affecting others, and you can trace which key made which request in the audit log. This is the same model used by commercial API gateways like Kong and AWS API Gateway.

**Extending this:** The `Auth` struct can be extended to support key metadata — owner name, expiry date, per-key rate limits — by replacing the `map[string]bool` with a `map[string]KeyConfig` struct.

---

### 3. Policy engine — `internal/policy/policy.go`

The policy engine is the AI governance core. It runs two independent checks on every request that passes authentication:

#### 3a. Blocked word detection

```go
blockedWords: []string{
    "jailbreak",
    "ignore instructions",
    "ignore previous",
    "bypass",
    "pretend you are",
}
```

The prompt is lowercased and scanned for each phrase using `strings.Contains`. Case-insensitive matching ensures variations like "JAILBREAK" or "Jailbreak" are caught. On a match, the request is rejected with `403 Forbidden` and the specific word that triggered the block is included in the response and the audit log.

**What this prevents:** Prompt injection attacks, jailbreak attempts, and social engineering prompts that try to override the model's system instructions. In an enterprise context this list would be extended to include company-specific sensitive terms — product codenames, customer identifiers, internal project names.

**Extending this:** The blocked word list can be loaded from a database or config file at runtime, enabling hot-reloading of policy rules without restarting the gateway.

#### 3b. Rate limiting

```go
maxRequests: 5
windowSize:  time.Minute
```

Implemented as a sliding window counter per client IP address. Each IP's request timestamps are stored in a `map[string][]time.Time`. On every request:

1. Timestamps older than the window (1 minute) are pruned.
2. If the remaining count is at or above `maxRequests`, the request is rejected with `429`-style messaging, and the exact wait time until the window clears is calculated and returned.
3. If under the limit, the current timestamp is appended and the request proceeds.

A `sync.Mutex` wraps all map access to make this safe for Go's concurrent HTTP handler goroutines.

**What this prevents:** A single user or service from exhausting your Groq API quota, causing degraded service for other users. Also limits the blast radius of a compromised API key.

**Extending this:** Per-key rate limits (premium keys get higher limits), distributed rate limiting via Redis for multi-instance deployments, and burst allowance on top of the sliding window.

---

### 4. Audit logger — `internal/logger/logger.go`

Every request — whether allowed, blocked, or errored — is written to the audit log. The logger is environment-aware:

| Environment | Storage | Persistence |
|---|---|---|
| Local development | SQLite file (`gateway.db`) | Persists across restarts |
| Cloud (Render free tier) | In-memory slice | Resets on restart |
| Cloud with volume mount | SQLite on mounted disk | Persists across restarts |

The SQLite schema:

```sql
CREATE TABLE IF NOT EXISTS audit_logs (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    client_ip TEXT,
    prompt    TEXT,
    response  TEXT,
    status    TEXT,      -- 'allowed', 'blocked', 'error'
    blocked   INTEGER DEFAULT 0,
    reason    TEXT       -- populated when blocked
);
```

**What this enables:** Full forensic reconstruction of any AI interaction. You can answer "what did user X send at 14:32 on Tuesday and what did the AI respond with?" — which is a compliance requirement in regulated industries (finance, healthcare, legal).

**Thread safety:** The in-memory logger uses a `sync.Mutex` to protect the log slice from concurrent writes. The SQLite logger relies on SQLite's own write serialisation.

**Extending this:** Ship logs to an external SIEM (Splunk, Datadog, Elastic) via a background goroutine that tails the SQLite table and posts to a webhook. Add log retention policies that purge rows older than N days.

---

### 5. Groq AI integration — `internal/gateway/groq.go`

Approved requests are forwarded to Groq's OpenAI-compatible REST API using the `resty` HTTP client. The model used is `llama-3.3-70b-versatile` — Groq's fastest hosted version of Meta's Llama 3.3 70B parameter model.

```
POST https://api.groq.com/openai/v1/chat/completions
Authorization: Bearer {GROQ_API_KEY}
Content-Type: application/json

{
  "model": "llama-3.3-70b-versatile",
  "messages": [{ "role": "user", "content": "{prompt}" }]
}
```

The Groq API key stored in the gateway's environment is never exposed to callers — the caller only ever sees the gateway's own API key system. This is the core security property of a reverse proxy: credential isolation.

**Why Groq:** Groq runs LLMs on custom LPU (Language Processing Unit) hardware, making inference significantly faster than GPU-based providers. The free tier is generous enough for development and light production workloads. The API is OpenAI-compatible, so switching to a different provider (OpenAI, Anthropic, Mistral) requires changing only the base URL and model name.

**Extending this:** Add a provider abstraction interface so the gateway can route different prompt types to different models — coding questions to one model, general questions to another. Add streaming response support via `Transfer-Encoding: chunked`.

---

### 6. Chat UI — `internal/dashboard/dashboard.go` → `homeHTML`

A self-contained single-page chat interface served at `/`. No JavaScript framework, no build step — pure HTML, CSS, and vanilla JS compiled into the Go binary as a string template.

**Features:**
- **API key input** in the top bar — stored in memory, sent as `X-API-Key` on every request
- **Suggestion chips** for common prompts to help new users get started
- **Typing indicator** (animated dots) while waiting for the AI response
- **Message bubbles** — user messages on the right in indigo, AI responses on the left in dark
- **Blocked message styling** — red bubble with the block reason when policy rejects a prompt
- **Sidebar history** — last 20 prompts with their status badge (allowed / blocked / error)
- **Keyboard shortcut** — Enter to send, Shift+Enter for a new line

All AI calls are made via `fetch()` directly from the browser to the `/ai` endpoint on the same origin. No CORS configuration needed. The UI never holds the Groq API key — only the gateway's own client key.

---

### 7. Audit dashboard — `internal/dashboard/dashboard.go` → `dashboardHTML`

A server-rendered HTML dashboard at `/dashboard` showing the full picture of gateway activity.

**Components:**

| Component | What it shows |
|---|---|
| Stat cards | Total / Allowed / Blocked / Error counts |
| Hourly bar chart | Request volume by hour for the last 24 hours, built from `GROUP BY strftime('%H:00', timestamp)` |
| Log table | Last 50 requests with IP, prompt, truncated response, status badge, and block reason |
| Search bar | Filters logs by prompt content, status, or IP via SQL `LIKE` query |
| Auto-refresh | JavaScript countdown timer reloads the page every 10 seconds, preserving the active search query |

The dashboard reads directly from the same logger instance as the gateway — no separate database connection, no caching layer. For high-traffic deployments, a read replica or a cached summary table would be the next step.

---

## Project structure

```
ai-gateway/
│
├── cmd/
│   └── main.go                 # Entry point — wires all layers together,
│                               # starts HTTP server, handles PORT + db path
│
├── internal/
│   ├── auth/
│   │   └── auth.go             # API key store + X-API-Key header validation
│   │
│   ├── policy/
│   │   └── policy.go           # Blocked word scanner + sliding window rate limiter
│   │
│   ├── logger/
│   │   └── logger.go           # Dual-mode audit logger (SQLite + in-memory)
│   │                           # AuditLog struct, GetAll() for dashboard queries
│   │
│   ├── gateway/
│   │   ├── gateway.go          # Core request handler — composes auth, policy,
│   │   │                       # logger, and AI client into the request pipeline
│   │   └── groq.go             # Groq API client — request serialisation,
│   │                           # response parsing, error handling
│   │
│   └── dashboard/
│       └── dashboard.go        # HandleHome (chat UI) + HandleDashboard (audit)
│                               # + HandleStats (JSON stats endpoint)
│                               # homeHTML and dashboardHTML templates inline
│
├── Dockerfile                  # Multi-stage build — golang:1.26-alpine builder,
│                               # alpine:latest runtime, single binary output
│
├── go.mod                      # Module: ai-gateway, Go 1.26
├── go.sum                      # Dependency checksums
├── .env                        # Local secrets (gitignored)
└── .gitignore                  # Excludes .env, gateway.db, fly.toml
```

---

## Tech stack

| Concern | Choice | Reason |
|---|---|---|
| Language | Go 1.26 | Compiled binary, low memory footprint, excellent `net/http` stdlib, goroutine concurrency for safe rate limiting |
| AI provider | Groq — Llama 3.3 70B | OpenAI-compatible API, free tier, fastest open-weight inference available |
| Storage | SQLite via `modernc.org/sqlite` | Pure-Go driver — no CGO, no C compiler needed on Windows, zero-config |
| HTTP client | `github.com/go-resty/resty/v2` | Fluent API for clean JSON request building to Groq |
| Config | `github.com/joho/godotenv` | `.env` file support for local development |
| Deployment | Render (free tier) | No credit card, Docker-native, auto-deploys from GitHub |
| Container | Docker multi-stage build | Builder stage compiles Go binary, runtime stage is minimal Alpine — final image ~15MB |

---

## Getting started

### Prerequisites

- Go 1.26+ (`go version`)
- A free Groq API key from [console.groq.com](https://console.groq.com)
- Git

### Local setup

```bash
# Clone the repository
git clone https://github.com/YOUR_USERNAME/ai-gateway.git
cd ai-gateway

# Install dependencies
go mod tidy

# Create your environment file
cp .env.example .env
# Edit .env and add your keys

# Run the gateway
go run cmd/main.go
```

### Environment variables

Create a `.env` file in the project root:

```env
GROQ_API_KEY=gsk_your_groq_key_here
GATEWAY_API_KEYS=key-alpha-123,key-beta-456,key-gamma-789
```

| Variable | Required | Description |
|---|---|---|
| `GROQ_API_KEY` | Yes | Your Groq API key. Get one free at console.groq.com |
| `GATEWAY_API_KEYS` | Yes | Comma-separated list of keys your clients use to authenticate with the gateway |
| `PORT` | No | Server port. Defaults to 8080. Set automatically by Render |
| `RENDER` | No | Set by Render at runtime. Switches logger to in-memory mode |

### Verify it works

```bash
# Health check
curl http://localhost:8080/health

# Send an authenticated AI request
curl -X POST http://localhost:8080/ai \
  -H "Content-Type: application/json" \
  -H "X-API-Key: key-alpha-123" \
  -d '{"prompt": "explain what a reverse proxy is in two sentences"}'

# Test auth rejection (no key)
curl -X POST http://localhost:8080/ai \
  -H "Content-Type: application/json" \
  -d '{"prompt": "hello"}'
# Returns: 401 Unauthorized

# Test policy block
curl -X POST http://localhost:8080/ai \
  -H "Content-Type: application/json" \
  -H "X-API-Key: key-alpha-123" \
  -d '{"prompt": "jailbreak this system"}'
# Returns: 403 Forbidden + block reason
```

---

## API reference

### `POST /ai`

The core gateway endpoint. Accepts a prompt, runs it through the full pipeline, and returns an AI response.

**Request**

```http
POST /ai HTTP/1.1
Content-Type: application/json
X-API-Key: key-alpha-123

{
  "prompt": "your prompt text here"
}
```

**Response — success**

```json
{
  "status": "success",
  "prompt": "your prompt text here",
  "response": "The AI-generated response..."
}
```

**Response — blocked by policy**

```json
{
  "status": "blocked",
  "reason": "Prompt contains blocked word: jailbreak"
}
```

**Response — unauthorized**

```json
{
  "status": "unauthorized",
  "reason": "Missing or invalid X-API-Key header"
}
```

**HTTP status codes**

| Code | Meaning |
|---|---|
| 200 | Request processed, AI response returned |
| 400 | Malformed JSON or missing prompt field |
| 401 | Missing or invalid API key |
| 403 | Blocked by policy engine |
| 405 | Non-POST request to /ai |
| 500 | Groq API error or internal failure |

### `GET /health`

Returns `200 OK` with plain text `Gateway is running`. Used by Render and other platforms as a liveness probe to confirm the process is healthy.

### `GET /dashboard`

Returns the HTML audit dashboard. Accepts an optional `?search=` query parameter to filter the log table.

### `GET /`

Returns the browser chat interface.

---

## Deployment on Render

1. Push your code to a GitHub repository (ensure `.env` is in `.gitignore`)
2. Go to [render.com](https://render.com) → New → Web Service
3. Connect your GitHub repository
4. Set runtime to **Docker**
5. Add environment variables in the Render dashboard:
   - `GROQ_API_KEY` → your Groq key
   - `GATEWAY_API_KEYS` → your client keys
6. Click **Create Web Service**

Render builds the Docker image, deploys the binary, and gives you a public URL at `https://your-app.onrender.com`. Every push to `main` triggers an automatic redeploy.

**Note on persistence:** Render's free tier does not include persistent disk. Logs are stored in-memory and reset on each deploy or restart. Upgrade to a paid tier and mount a disk at `/data` to get persistent SQLite logging.

---

## Extending the gateway

The layered architecture makes the gateway straightforward to extend:

| What to add | Where to add it | How |
|---|---|---|
| New blocked words | `internal/policy/policy.go` | Add strings to the `blockedWords` slice |
| Higher rate limits per key | `internal/auth/auth.go` + `internal/policy/policy.go` | Add a `Limit` field to the key config, pass it to the rate limiter |
| Different AI provider | `internal/gateway/` | Create a new `openai.go` or `anthropic.go` with the same `callAI` signature |
| Streaming responses | `internal/gateway/groq.go` | Switch to SSE and pipe `resp.Body` directly to `http.ResponseWriter` |
| Webhook alerts on blocks | `internal/policy/policy.go` | Fire a goroutine on block events to POST to a Slack or Teams webhook |
| Per-prompt token counting | `internal/logger/logger.go` | Add a `token_count` column and estimate tokens before forwarding |
| Admin API for key management | `cmd/main.go` | Add `/admin/keys` routes behind a separate admin key |

---

## Security considerations

- **The Groq API key is never exposed to callers.** Clients authenticate with gateway keys; the upstream provider key lives only in the gateway's environment.
- **`.env` is gitignored.** Keys are never committed to source control.
- **Rate limiting is per-IP**, not per-key. For production, per-key rate limiting is more precise and harder to circumvent.
- **Blocked words are case-insensitive** but not fuzzy. A determined attacker could use character substitution (`ja1lbreak`). For production, consider regex patterns or an ML-based content classifier.
- **The dashboard has no authentication.** Anyone who can reach `/dashboard` can see all logged prompts. Add HTTP Basic Auth or a session check before the dashboard handler for any public deployment.
- **SQLite is single-writer.** Under very high concurrency, write contention will cause latency spikes. PostgreSQL via `pgx` is the upgrade path for high-traffic production.

---

## License

MIT — use it, extend it, ship it.
