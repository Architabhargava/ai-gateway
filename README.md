# AI Gateway — EU AI Act Compliant

A production-grade AI API gateway built in Go that enforces EU AI Act compliance on every request. Every prompt is authenticated, classified for intent using a live AI classifier, risk-scored, and logged with a full reasoning chain before being forwarded to Groq's Llama 3.3 70B model.

**Live demo:** https://ai-gateway-wd6e.onrender.com  
**Admin platform:** https://ai-gateway-wd6e.onrender.com/platform *(credentials required)*  
**Metrics dashboard:** https://ai-gateway-wd6e.onrender.com/metrics-dashboard *(credentials required)*

---

## What it does

Most AI integrations send prompts directly to a model with no governance layer. This gateway sits between your users and the AI model and enforces seven compliance features:

| Feature | What it does |
|---|---|
| Decision audit trail | Every request logs the AI classifier's full step-by-step reasoning chain, risk level, and EU AI Act article citation |
| Prohibited use detector | Detects EU AI Act Article 5 banned use cases and returns HTTP 451 with the precise EUR-Lex legal reference |
| Human review queue | Borderline and sensitive-category requests are held for human approval — satisfies Article 14 human oversight |
| Incident reporting | Every high-confidence block creates a severity-graded incident with email alerts via Resend |
| Log retention + GDPR erasure | Configurable retention policy with nightly purge and Article 17 right-to-erasure per API key |
| API key management | Per-customer cryptographically random keys with generate, revoke, suspend, activate lifecycle |
| Metrics dashboard | Live Chart.js observability dashboard with 6 charts — request rates, block rates, risk breakdown, score distribution |

---

## Architecture

```
Request (X-API-Key: gw_...)
    │
    ▼
KeyManager — validates gw_ key, checks status (active/suspended/revoked/expired)
    │
    ▼
Rate limiter — per-key sliding window (configurable req/min per customer)
    │
    ▼
Article 5 detector — fast keyword pre-screen + Groq AI deep check
    │ match → 451 Unavailable For Legal Reasons + EUR-Lex citation + critical incident
    │
    ▼
AI intent classifier — Groq Llama 3.3 70B (temperature 0.0)
    │ score ≥ 0.75       → 403 blocked + incident + Resend email alert
    │ sensitive category → human review queue (Article 14)
    │ borderline 0.4–0.75 → human review queue (Article 14)
    │ score < 0.4        → forwarded to Groq
    │
    ▼
Groq Llama 3.3 70B — response returned to user
    │
    ▼
SQLite audit log — reasoning chain, risk level, EU article, classifier score
Prometheus metrics — counters, histograms, gauges updated on every request
```

---

## Two separate interfaces

### User interface — `GET /`
Clean chat UI for customers. Authenticate with a `gw_` prefixed gateway key. No audit logs, no incidents, no other users' data visible. The Groq API key is never exposed.

### Admin platform — `GET /platform`
Protected by HTTP Basic Auth (constant-time comparison). Full governance dashboard:

| Section | What it shows |
|---|---|
| Chat | Send prompts, see AI classifier reasoning on blocks |
| Audit Log | Searchable table, risk filter, hourly chart, EU article citations |
| Review Queue | Pending items with Approve/Reject buttons, live badge count |
| Incidents | Severity-graded incidents, resolve workflow, email sent indicator |
| API Keys | Generate/revoke/suspend/activate per customer, usage tracking |
| Retention & GDPR | Storage stats, retention days config, GDPR erasure |
| Metrics | Live Chart.js dashboard with 6 charts |

---

## Tech stack

| Layer | Technology |
|---|---|
| Language | Go 1.24 |
| AI model | Groq — Llama 3.3 70B Versatile |
| Database | SQLite (modernc.org/sqlite — pure Go, no CGO) |
| Charts | Chart.js 4.4 (CDN) |
| Metrics | Prometheus client_golang |
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
│   └── main.go                         # Entry point, graceful shutdown (SIGTERM/SIGINT)
├── internal/
│   ├── auth/
│   │   ├── admin.go                    # HTTP Basic Auth middleware (constant-time)
│   │   ├── auth.go                     # Auth adapter over KeyManager
│   │   ├── keymanager.go               # API key lifecycle — generate/revoke/suspend/activate
│   │   ├── auth_test.go                # Admin auth tests (timing attack resistance)
│   │   └── keymanager_test_helpers.go  # Test key seeding without DB
│   ├── compliance/
│   │   ├── prohibited.go               # EU AI Act Article 5 detector (2-stage)
│   │   ├── incidents.go                # Incident reporting + Resend email alerts
│   │   ├── review.go                   # Human review queue (DB polling)
│   │   └── retention.go                # Log retention + GDPR right to erasure
│   ├── dashboard/
│   │   ├── platform.go                 # Unified admin platform UI (7 sections)
│   │   ├── chat.go                     # Public user chat UI
│   │   └── metrics_page.go             # Metrics dashboard (Chart.js)
│   ├── gateway/
│   │   ├── gateway.go                  # Core request pipeline + all admin handlers
│   │   ├── groq.go                     # Groq API client
│   │   ├── dashboard_api.go            # Metrics data API (/admin/metrics-data)
│   │   ├── gateway_test.go             # Integration tests
│   │   ├── gateway_helpers_test.go     # Test gateway factory
│   │   └── groq_test_helpers.go        # Mock Groq server for tests
│   ├── logger/
│   │   └── logger.go                   # SQLite audit log with EU AI Act fields
│   ├── metrics/
│   │   └── metrics.go                  # Prometheus counters, histograms, gauges
│   └── policy/
│       ├── policy.go                   # Per-key rate limiter + AI classifier
│       └── policy_test.go              # Rate limiter tests (8 cases, race detector)
├── .github/
│   └── workflows/
│       └── ci.yml                      # 4-job pipeline: test, build, docker+trivy, lint
├── Dockerfile                          # Multi-stage, non-root (nobody), HEALTHCHECK
├── docker-compose.yml                  # Local development
├── render.yaml                         # Render Infrastructure as Code
├── .golangci.yml                       # Lint configuration
├── Makefile                            # make run/build/test/cover/lint/docker/clean
├── .env.example                        # Environment variable template
└── TECHNICAL_DOCUMENTATION.md         # Complete technical deep-dive
```

---

## API reference

### Public endpoints

```
POST /ai
  Header: X-API-Key: gw_...
  Body:   {"prompt": "your prompt here"}

  200: {"status": "success", "response": "...", "risk_level": "minimal"}
  401: {"status": "unauthorized", "reason": "..."}
  403: {"status": "blocked", "reason": "...", "reasoning_chain": "...", "eu_article": "..."}
  429: {"status": "blocked", "reason": "rate limit exceeded — retry in Xs"}
  451: {"status": "unavailable_for_legal_reasons", "article": "Article 5(1)(c)",
        "legal_reference": "https://eur-lex.europa.eu/..."}

GET /health        → 200 ok
GET /metrics       → Prometheus text format metrics
```

### Admin API (Basic Auth required)

```
GET    /admin/audit/:id                    Full reasoning chain for a request
GET    /admin/keys                         List all API keys (?status=active)
GET    /admin/keys/stats                   Key statistics by status
POST   /admin/keys/generate                Create new key {name, owner, rate_limit, expires_days}
POST   /admin/keys/revoke                  Revoke {key_or_id, revoked_by, reason}
POST   /admin/keys/suspend                 Suspend {id, by}
POST   /admin/keys/activate                Re-activate {id, by}
GET    /admin/incidents                    List incidents (?severity=high)
GET    /admin/incidents/stats              Incident counts by severity
POST   /admin/incidents/resolve            Resolve {id, resolved_by}
GET    /admin/review                       Pending review items
GET    /admin/review/all                   All items (all statuses)
GET    /admin/review/stats                 Queue counts by status
POST   /admin/review/approve               Approve {id, reviewer}
POST   /admin/review/reject                Reject {id, reviewer}
GET    /admin/retention                    Current policy + storage stats
POST   /admin/retention                    Update {days, updated_by}
POST   /admin/retention/purge              Manual purge now
DELETE /admin/retention/erase              GDPR erasure {api_key}
GET    /admin/metrics-data                 Aggregated metrics JSON for dashboard
```

---

## Prometheus metrics

The `/metrics` endpoint exposes Prometheus-format metrics scraped by any compatible collector:

```
gateway_requests_total{status}              — counter, labelled by outcome
gateway_blocked_requests_total{category}    — counter, labelled by block category
gateway_prohibited_use_total{article}       — counter, labelled by EU AI Act article
gateway_rate_limit_hits_total{api_key_name} — counter, per key
gateway_request_duration_seconds{status}    — histogram, end-to-end latency
gateway_classifier_duration_seconds         — histogram, AI classifier latency
gateway_groq_duration_seconds               — histogram, Groq API latency
gateway_review_queue_wait_seconds           — histogram, time waiting for human decision
gateway_review_queue_pending                — gauge, current pending items
gateway_incidents_unresolved                — gauge, open incidents
gateway_active_api_keys                     — gauge, active key count
gateway_requests_by_risk_level_total{risk}  — counter, by EU AI Act risk level
```

---

## EU AI Act compliance mapping

| EU AI Act Article | Implementation |
|---|---|
| Article 5 — Prohibited practices | Two-stage detector: keyword pre-screen + Groq AI deep check. HTTP 451 with EUR-Lex citation |
| Article 9 — Risk management | AI classifier assigns risk levels: minimal / limited / high / unacceptable |
| Article 13 — Transparency | Every decision logged with full reasoning chain, indicators, confidence score |
| Article 14 — Human oversight | Review queue holds borderline/sensitive requests for human approval |
| Article 17 — Record keeping | Configurable retention policy with nightly purge goroutine |
| Article 52 — Transparency obligations | Users see risk level and EU article on every blocked response |
| GDPR Article 17 — Right to erasure | DELETE /admin/retention/erase removes all audit logs for a given API key |

---

## Running locally

**Prerequisites:** Go 1.24+, Groq API key (free at console.groq.com)

```bash
git clone https://github.com/Architabhargava/ai-gateway
cd ai-gateway
cp .env.example .env
# Edit .env with your keys
make run
```

**Run tests:**
```bash
make test
```

**Build binary:**
```bash
make build
```

**Docker:**
```bash
make docker
```

---

## CI/CD pipeline

Every push to `main` triggers a 4-job GitHub Actions pipeline:

```
Test   → go vet + go test -race + coverage report
Build  → CGO_ENABLED=0 binary, uploaded as artifact (5.3MB)
Docker → image build + Trivy CVE scan, report uploaded
Lint   → golangci-lint (advisory, non-blocking)
```

---

## Key design decisions

**Gateway key model — not BYOK**  
Users authenticate with gateway-issued `gw_` keys. The Groq API key is server-side only — users cannot bypass governance by calling Groq directly.

**Fail-open policy for classifier**  
If the Groq classifier is unreachable, requests are allowed through. Availability is prioritised over the safety classifier.

**HTTP 451 for Article 5 violations**  
RFC 7725 defines 451 for legally prohibited content — not 403 which implies authorisation failure.

**Constant-time admin auth**  
`crypto/subtle.ConstantTimeCompare` prevents timing attacks on credential validation.

**Per-key rate limiting**  
Buckets are keyed by API key value, not IP — correct for corporate NAT and shared networks.

**Database polling for review queue**  
Pure DB polling over in-memory channels — works correctly in multi-instance deployments.

**Lint non-blocking in CI**  
`continue-on-error: true` on lint job — lint issues are advisory warnings, not deployment blockers. The critical path is test → build → docker.

---

## Author

**Archita Bhargava**  
[GitHub](https://github.com/Architabhargava) · [LinkedIn](https://linkedin.com/in/archita-bhargava)