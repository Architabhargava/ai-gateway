# AI Gateway — EU AI Act Compliant

A production-grade AI API gateway built in Go that enforces EU AI Act compliance on every request. Every prompt is authenticated, classified for intent using a live AI classifier, risk-scored, and logged with a full reasoning chain before being forwarded to Groq's Llama 3.3 70B model.

**Live demo:** https://ai-gateway-wd6e.onrender.com  
**Admin platform:** https://ai-gateway-wd6e.onrender.com/platform *(credentials required)*

---

## What it does

Most AI integrations send prompts directly to a model with no governance layer. This gateway sits between your users and the AI model and enforces five compliance features required by the EU AI Act:

| Feature | What it does |
|---|---|
| Decision audit trail | Every request logs the AI classifier's full step-by-step reasoning chain, risk level, and EU AI Act article citation |
| Prohibited use detector | Detects EU AI Act Article 5 banned use cases (social scoring, biometric surveillance, emotion recognition at work) and returns HTTP 451 with the precise legal reference |
| Human review queue | Borderline and sensitive-category requests are held for human approval before being forwarded — satisfies Article 14 human oversight requirement |
| Incident reporting | Every high-confidence block creates a severity-graded incident with email alerts via Resend |
| Log retention + GDPR erasure | Configurable retention policy with nightly purge and Article 17 right-to-erasure per API key |

---

## Architecture

```
Request (X-API-Key)
    │
    ▼
Auth — validates managed gw_ key from api_keys table
    │
    ▼
Rate limiter — per-key sliding window (configurable req/min)
    │
    ▼
Article 5 detector — fast keyword pre-screen + AI deep check
    │ match → 451 Unavailable For Legal Reasons + EUR-Lex citation
    │
    ▼
AI intent classifier — Groq Llama 3.3 70B
    │ score ≥ 0.75    → 403 blocked + incident created + email alert
    │ sensitive cat.  → human review queue (Article 14)
    │ score 0.4–0.75  → human review queue (Article 14)
    │ score < 0.4     → forwarded to Groq
    │
    ▼
Groq Llama 3.3 70B — response returned to user
    │
    ▼
SQLite audit log — full reasoning chain, risk level, EU article stored
```

---

## Tech stack

| Layer | Technology |
|---|---|
| Language | Go 1.24 |
| AI model | Groq — Llama 3.3 70B Versatile |
| Database | SQLite (modernc.org/sqlite — pure Go, no CGO) |
| Email alerts | Resend API |
| Deployment | Render (Docker) |
| CI/CD | GitHub Actions — 4-job pipeline |
| Container | Docker multi-stage, Alpine runtime, non-root user |
| Security scan | Trivy CVE scanning on every push |

---

## Project structure

```
ai-gateway/
├── cmd/
│   └── main.go                    # Entry point, graceful shutdown
├── internal/
│   ├── auth/
│   │   ├── admin.go               # HTTP Basic Auth middleware
│   │   ├── auth.go                # Auth adapter
│   │   ├── keymanager.go          # API key lifecycle management
│   │   └── auth_test.go           # Admin auth tests
│   ├── compliance/
│   │   ├── prohibited.go          # EU AI Act Article 5 detector
│   │   ├── incidents.go           # Incident reporting + Resend alerts
│   │   ├── review.go              # Human review queue
│   │   └── retention.go           # Log retention + GDPR erasure
│   ├── dashboard/
│   │   ├── platform.go            # Unified admin platform UI
│   │   └── chat.go                # Public user chat UI
│   ├── gateway/
│   │   ├── gateway.go             # Core request pipeline
│   │   ├── groq.go                # Groq API client
│   │   └── gateway_test.go        # Gateway integration tests
│   ├── logger/
│   │   └── logger.go              # Audit log with EU AI Act fields
│   └── policy/
│       ├── policy.go              # Rate limiter + AI classifier
│       └── policy_test.go         # Rate limiter tests
├── .github/
│   └── workflows/
│       └── ci.yml                 # GitHub Actions CI pipeline
├── Dockerfile                     # Multi-stage, non-root, HEALTHCHECK
├── docker-compose.yml             # Local development
├── render.yaml                    # Render Infrastructure as Code
├── .golangci.yml                  # Lint configuration
└── Makefile                       # make run/build/test/lint/docker
```

---

## Two separate interfaces

### User interface — `GET /`
Clean chat UI where customers interact with the AI. Users authenticate with a `gw_` prefixed gateway key. They never see audit logs, incidents, or other users' data. The Groq API key is never exposed.

### Admin platform — `GET /platform`
Protected by HTTP Basic Auth (constant-time comparison to prevent timing attacks). Full governance dashboard with six sections:

- **Chat** — send prompts and see the AI classifier's reasoning chain on every block
- **Audit Log** — searchable table with risk filter, hourly chart, EU article citations
- **Review Queue** — live-polling queue with Approve/Reject buttons (Article 14)
- **Incidents** — severity-graded incident dashboard with resolve workflow
- **API Keys** — generate, revoke, suspend, activate keys per customer
- **Retention & GDPR** — configure retention days, manual purge, right to erasure

---

## API reference

### Public endpoints

```
POST /ai
  Header: X-API-Key: gw_...
  Body:   {"prompt": "your prompt here"}

  Response 200: {"status": "success", "response": "...", "risk_level": "minimal"}
  Response 401: {"status": "unauthorized", "reason": "..."}
  Response 403: {"status": "blocked", "reason": "...", "reasoning_chain": "..."}
  Response 429: {"status": "blocked", "reason": "rate limit exceeded"}
  Response 451: {"status": "unavailable_for_legal_reasons", "article": "Article 5(1)(c)", "legal_reference": "https://eur-lex.europa.eu/..."}

GET /health → 200 ok
```

### Admin API (Basic Auth required)

```
GET    /admin/audit/:id              Full reasoning chain for a request
GET    /admin/keys                   List all API keys
POST   /admin/keys/generate          Create new key {name, owner, rate_limit, expires_days}
POST   /admin/keys/revoke            Revoke key {key_or_id, reason}
POST   /admin/keys/suspend           Suspend key {id}
POST   /admin/keys/activate          Re-activate suspended key {id}
GET    /admin/incidents              List incidents (filter: ?severity=high)
POST   /admin/incidents/resolve      Resolve incident {id}
GET    /admin/review                 Pending review items
POST   /admin/review/approve         Approve item {id, reviewer}
POST   /admin/review/reject          Reject item {id, reviewer}
GET    /admin/retention              Current policy + storage stats
POST   /admin/retention              Update retention days {days}
POST   /admin/retention/purge        Manual purge
DELETE /admin/retention/erase        GDPR erasure {api_key}
```

---

## EU AI Act compliance mapping

| EU AI Act Article | Implementation |
|---|---|
| Article 5 — Prohibited practices | Two-stage detector: keyword pre-screen + Groq AI deep check. Returns HTTP 451 with EUR-Lex citation |
| Article 9 — Risk management | AI classifier assigns risk levels: minimal / limited / high / unacceptable |
| Article 13 — Transparency | Every decision logged with full reasoning chain, indicators, and confidence score |
| Article 14 — Human oversight | Review queue holds borderline requests for human approval before forwarding |
| Article 17 — Record keeping | Configurable retention policy with nightly purge goroutine |
| Article 52 — Transparency obligations | Users see risk level and EU article on every blocked response |
| GDPR Article 17 — Right to erasure | DELETE /admin/retention/erase removes all audit logs for a given API key |

---

## Running locally

**Prerequisites:** Go 1.24+, a Groq API key (free at console.groq.com)

```bash
# Clone
git clone https://github.com/Architabhargava/ai-gateway
cd ai-gateway

# Configure
cp .env.example .env
# Edit .env with your keys

# Run
make run

# Test
make test

# Build binary
make build

# Docker
make docker
```

**.env.example:**
```
GROQ_API_KEY=gsk_your_groq_key_here
GATEWAY_API_KEYS=key-alpha-123,key-beta-456
RESEND_API_KEY=re_your_resend_key_here
ALERT_EMAIL_TO=your@email.com
ALERT_EMAIL_FROM=onboarding@resend.dev
ADMIN_USERNAME=admin
ADMIN_PASSWORD=choose_a_strong_password
```

---

## CI/CD pipeline

Every push to `main` triggers a 4-job GitHub Actions pipeline:

```
Test ──────────────────────────────────→ ✅ go vet + race detector + coverage
    └──→ Build ────────────────────────→ ✅ CGO_ENABLED=0 binary, uploaded as artifact
              └──→ Docker + Trivy ─────→ ✅ image build + CVE scan, report uploaded
Lint ──────────────────────────────────→ ⚠️  advisory only, never blocks deploy
```

---

## Key design decisions

**Gateway key model — not BYOK (bring your own key)**  
Users authenticate with gateway-issued `gw_` keys. The Groq API key is server-side only. Users cannot bypass governance by calling Groq directly — all requests must go through the gateway's classification pipeline.

**Fail-open policy for classifier**  
If the Groq classifier is unreachable, requests are allowed through. Availability is prioritised over the safety classifier because a down classifier should not take down the entire gateway.

**HTTP 451 for Article 5 violations**  
RFC 7725 defines 451 Unavailable For Legal Reasons specifically for content blocked due to legal obligations. This is the correct status code — not 403 — for EU AI Act Article 5 prohibited use cases.

**Constant-time admin auth**  
`crypto/subtle.ConstantTimeCompare` is used for credential validation to prevent timing attacks that could leak whether the username or password is wrong.

**Per-key rate limiting**  
Rate limit buckets are keyed by API key value, not IP address. This means each customer has their own independent rate limit regardless of how many users share an IP (corporate NAT, shared WiFi).

---

## Test coverage

```
internal/auth     — AdminAuth middleware, timing attack resistance
internal/policy   — Rate limiter: sliding window, per-key isolation, concurrency
internal/gateway  — HandleAI: method validation, auth rejection, JSON parsing, rate limits
```

Run with: `go test ./...` or `make test`

---

## Deployment

Deployed on Render using Docker runtime. The `render.yaml` file defines the full infrastructure as code — runtime, health check path, and all environment variable keys.

The free tier uses `/tmp/gateway.db` for SQLite (ephemeral — resets on redeploy). For production persistence, migrate to PostgreSQL using Render's managed database or Supabase.

---

## Author

**Archita Bhargava**  
[GitHub](https://github.com/Architabhargava) · [LinkedIn](https://linkedin.com/in/archita-bhargava)