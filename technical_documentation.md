# AI Gateway — Technical Documentation

**Author:** Archita Bhargava  
**Stack:** Go 1.24 · Groq · SQLite · Docker · GitHub Actions · Render  
**Live:** https://ai-gateway-wd6e.onrender.com  
**Repository:** https://github.com/Architabhargava/ai-gateway

---

## Table of Contents

1. [Why this project exists](#1-why-this-project-exists)
2. [Why Groq alone is not enough](#2-why-groq-alone-is-not-enough)
3. [System architecture](#3-system-architecture)
4. [Tech stack — every choice explained](#4-tech-stack--every-choice-explained)
5. [Feature deep-dives](#5-feature-deep-dives)
6. [The request pipeline — step by step](#6-the-request-pipeline--step-by-step)
7. [Security design](#7-security-design)
8. [DevOps and CI/CD](#8-devops-and-cicd)
9. [Challenges and how I solved them](#9-challenges-and-how-i-solved-them)
10. [What I learned](#10-what-i-learned)
11. [Why this matters in the real world](#11-why-this-matters-in-the-real-world)

---

## 1. Why this project exists

When companies integrate AI into their products today, the typical architecture looks like this:

```
User → Your application → OpenAI / Groq / Anthropic → Response back to user
```

This is a direct integration. The AI model is called inline. There is no audit trail of what was sent. There is no way to block prohibited content before it reaches the model. There is no record of why a decision was made. There is no human review for ambiguous cases. There is no way to revoke access for a misbehaving user without changing code. And there is certainly no compliance with the EU AI Act, which came into force in 2024 and will be enforced with fines up to 35 million euros or 7% of global annual turnover.

This gateway exists to solve all of those problems in a single layer that sits between your users and the AI model. It does not change how you call the AI. It does not change what the AI returns. It adds a governance layer that ensures every interaction is authenticated, classified, audited, and compliant — before any prompt reaches the model and after every response comes back.

The inspiration was straightforward: every serious API product in the world — OpenAI, Anthropic, Stripe, Twilio — issues its own keys to customers rather than giving customers direct access to the underlying infrastructure. The governance, rate limiting, abuse detection, and billing all happen at the gateway layer. I wanted to build exactly that, but designed specifically for AI workloads and the compliance requirements that come with them.

---

## 2. Why Groq alone is not enough

Groq is an AI inference platform. It is exceptionally good at what it does — running large language models at very high speed. Llama 3.3 70B on Groq is fast, capable, and free to start with. But Groq is a model provider, not a governance system. It has no concept of:

**Who is making the request**  
Groq accepts an API key. That API key belongs to you, the developer. If you share it with users, all users share the same identity. You cannot tell which user sent which prompt. You cannot rate-limit one user without rate-limiting all of them. You cannot revoke access for one user. You cannot track that User A sent 10,000 requests while User B sent 3.

**Whether the request is legal**  
Groq has content moderation, but it is not calibrated to the EU AI Act. It does not know that a request asking you to build a system that scores citizens based on social behaviour is an Article 5(1)(c) violation. It will not return an HTTP 451. It will not cite EUR-Lex. It will not create an incident record. It will not alert your security team by email.

**Why a decision was made**  
When Groq's content filter blocks something, you get a refusal message. You do not get a structured audit record with a reasoning chain, a risk level, a confidence score, and an EU AI Act article citation that you can query six months later when a regulator asks why that prompt was blocked.

**Whether borderline cases need a human**  
Some prompts are not clearly harmful and not clearly safe. They exist in a zone where the right answer depends on context, intent, and judgment. Groq cannot route these to a human reviewer. It either blocks or allows. This gateway routes them to a human review queue where an operator can approve or reject with a full record of the decision.

**Your users' data retention rights**  
If a user invokes GDPR Article 17 and asks you to delete all data associated with their account, Groq has no mechanism for this. Your gateway does — a single API call erases all audit log entries for a given API key.

In short: Groq is the engine. This gateway is the car — the steering, brakes, seatbelts, dashboard, and compliance systems that make the engine usable in the real world.

---

## 3. System architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         PUBLIC INTERNET                          │
└──────────────────────┬──────────────────────────────────────────┘
                       │
              ┌────────▼────────┐
              │   User (browser) │  ← Paste gw_ key, send prompt
              └────────┬────────┘
                       │ POST /ai  X-API-Key: gw_abc123
                       │
┌──────────────────────▼──────────────────────────────────────────┐
│                      AI GATEWAY (Go 1.24)                        │
│                                                                   │
│  ┌─────────────┐   ┌──────────────┐   ┌─────────────────────┐   │
│  │ KeyManager  │   │ Rate Limiter │   │ Article 5 Detector  │   │
│  │ (SQLite)    │   │ (per-key     │   │ (keyword + AI)      │   │
│  │             │   │  sliding     │   │                     │   │
│  │ Validates   │   │  window)     │   │ 451 if prohibited   │   │
│  │ gw_ keys    │   │              │   │ + EUR-Lex citation  │   │
│  └─────────────┘   └──────────────┘   └─────────────────────┘   │
│                                                                   │
│  ┌──────────────────────────────────────────────────────────┐    │
│  │                  AI Intent Classifier                     │    │
│  │                  (Groq Llama 3.3 70B)                    │    │
│  │                                                           │    │
│  │  score ≥ 0.75 → auto-block + incident + email alert     │    │
│  │  sensitive    → human review queue (Article 14)          │    │
│  │  borderline   → human review queue (Article 14)          │    │
│  │  score < 0.4  → forward to Groq                         │    │
│  └──────────────────────────────────────────────────────────┘    │
│                                                                   │
│  ┌─────────────┐   ┌──────────────┐   ┌─────────────────────┐   │
│  │ SQLite      │   │ Incident     │   │ Resend Email        │   │
│  │ Audit Log   │   │ Manager      │   │ Alerts              │   │
│  │             │   │              │   │                     │   │
│  │ Full chain  │   │ Severity     │   │ medium/high/        │   │
│  │ + EU article│   │ grading      │   │ critical only       │   │
│  └─────────────┘   └──────────────┘   └─────────────────────┘   │
│                                                                   │
└──────────────────────┬──────────────────────────────────────────┘
                       │
              ┌────────▼────────┐
              │  Groq API       │  ← Only reached if all checks pass
              │  Llama 3.3 70B  │
              └────────┬────────┘
                       │
              ┌────────▼────────┐
              │  Response to    │
              │  user           │
              └─────────────────┘
```

**Separation of concerns:**

The gateway has two completely separate interfaces. The public interface at `/` is a chat UI where users authenticate with their `gw_` key and send prompts. The admin interface at `/platform` is protected by HTTP Basic Auth and gives operators full visibility into audit logs, incidents, the review queue, key management, and data retention. A user who discovers the `/platform` URL gets a browser login prompt they cannot pass without the admin credentials.

---

## 4. Tech stack — every choice explained

### Go 1.24

Go was chosen over Node.js, Python, or Java for several specific reasons.

**Concurrency model:** The human review queue requires holding an HTTP connection open while polling a database for a decision. In Node.js this is trivial with async/await but creates callback complexity. In Python with Flask it would require threading. In Go, each HTTP request runs in its own goroutine — lightweight, managed by the Go scheduler, and capable of blocking on a database poll for 5 minutes without affecting other requests. The gateway can handle thousands of concurrent review-pending requests without additional infrastructure.

**Single binary deployment:** `go build` produces a single statically compiled binary with zero runtime dependencies. The Docker image is 12MB. There is no Node.js runtime to install, no Python virtual environment, no JVM warmup. The container starts in under a second.

**Standard library completeness:** The entire HTTP server, router, JSON encoding, and cryptographic functions come from Go's standard library. The only external dependencies are the SQLite driver and the `.env` loader. Fewer dependencies means fewer CVEs, fewer version conflicts, and a smaller attack surface.

**Performance:** Go's HTTP server handles tens of thousands of requests per second on a single core. For an API gateway where latency matters, this is meaningful.

### Groq — Llama 3.3 70B Versatile

Groq is used for two distinct purposes in this system, which is an important design choice:

**The AI classifier** — every incoming prompt is sent to Llama 3.3 70B with a detailed system prompt asking it to reason step-by-step about safety threats, assign a risk score, identify the EU AI Act article that applies, and return a structured JSON verdict. The model is called with `temperature: 0.0` to ensure deterministic, consistent classification decisions.

**The response model** — for prompts that pass all checks, the same model is called to generate the actual response. This means one API key, one model, two purposes.

The choice of Llama 3.3 70B over GPT-4 or Claude was pragmatic: Groq's free tier is generous, the inference speed is industry-leading, and the model is capable enough for both classification and generation. For a governance system where the classifier runs on every single request, cost and latency matter significantly.

### SQLite with modernc.org/sqlite

SQLite is an embedded database — it runs inside the Go process with no separate database server. The `modernc.org/sqlite` driver is a pure Go implementation with no CGO dependency, which means the binary compiles cleanly on all platforms without a C compiler.

The database stores five tables: `audit_logs`, `api_keys`, `incidents`, `review_queue`, and `retention_policy`. For a single-server deployment handling thousands of requests per day, SQLite is not a limitation — it handles concurrent reads well and sequential writes are fast enough for audit logging.

The acknowledged limitation is that SQLite on Render's free tier is ephemeral — it resets on redeploy because there is no persistent disk. The documented migration path is PostgreSQL via Render's managed database or Supabase's free tier.

### Docker — multi-stage build

The Dockerfile uses a two-stage build:

**Stage 1 (builder):** Uses `golang:1.24-alpine` which includes the Go toolchain. Downloads dependencies, copies source, compiles with `CGO_ENABLED=0` to produce a fully static binary. The `-s -w` ldflags strip debug symbols, reducing binary size by approximately 30%.

**Stage 2 (runtime):** Uses `alpine:3.20` — a 5MB base image with no Go toolchain. Only the compiled binary is copied in. The container runs as the `nobody` user (uid 65534) rather than root. If an attacker exploits a vulnerability in the application, they have no filesystem write permissions and cannot escalate privileges.

The HEALTHCHECK instruction causes Docker, docker-compose, and Render to periodically call `/health` and restart the container if it stops responding.

### GitHub Actions — 4-job CI pipeline

The pipeline runs on every push to `main`:

**Test job:** Runs `go vet` (static analysis), then `go test -race` (race condition detection) across all packages. The race detector instruments memory accesses and reports data races at runtime — critical for a concurrent system where the rate limiter, review queue, and incident manager all run concurrently.

**Build job:** Compiles a production binary with the git commit hash injected as the version string via ldflags. Uploads the binary as a GitHub Actions artifact downloadable for 7 days.

**Docker + Trivy job:** Builds the Docker image and runs Trivy's CVE scanner against it. Exit code 0 means the scan never fails the pipeline — it produces a report that is uploaded as an artifact and printed to the log. This gives you a security posture report on every push without blocking deployments for informational CVEs.

**Lint job:** Runs golangci-lint with `continue-on-error: true`. Lint failures are advisory warnings, not deployment blockers. The critical path is Test → Build → Docker.

### Render

Render was chosen over AWS, GCP, and Heroku for a specific reason: `render.yaml` allows the entire deployment to be defined as infrastructure as code in the repository. Anyone who forks the repo and connects it to Render gets the same deployment configuration — environment variable keys, health check path, Docker runtime — without manual dashboard configuration.

---

## 5. Feature deep-dives

### Feature 1 — Decision audit trail with full reasoning chain

**The problem it solves:**  
When an AI system makes a decision — blocking a request, allowing it, routing it for review — regulators, operators, and users have a legitimate interest in understanding why. "The AI blocked it" is not an acceptable answer under the EU AI Act. Article 13 requires transparency and the provision of information to deployers.

**How it works:**  
The AI classifier is instructed via its system prompt to reason step-by-step before returning its verdict. The reasoning is structured as a pipe-delimited chain: `Step 1: What is the literal request? | Step 2: Most charitable interpretation? | Step 3: Most adversarial interpretation? | Step 4: Which is more likely? | Step 5: Would fulfilling this cause harm?`

This reasoning chain, along with the risk level, EU AI Act article, confidence score, and category, is stored in the `audit_logs` table on every single request — blocked, allowed, and review-pending. The admin platform exposes a searchable table with these fields and a `View →` link that shows the complete record for any individual request.

**The mindset:**  
Auditability is not an afterthought — it is a first-class feature. Every compliance system in financial services, healthcare, and legal tech is built around the principle that you must be able to reconstruct every decision after the fact. This feature applies that principle to AI.

**Schema additions:**
```sql
ALTER TABLE audit_logs ADD COLUMN reasoning_chain  TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN risk_level       TEXT NOT NULL DEFAULT 'minimal';
ALTER TABLE audit_logs ADD COLUMN eu_article       TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN category         TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN classifier_score REAL NOT NULL DEFAULT 0.0;
```

---

### Feature 2 — Prohibited use case detector

**The problem it solves:**  
EU AI Act Article 5 defines seven categories of AI use that are outright banned — not restricted, not requiring special approval, but banned. Social scoring by public authorities. Real-time biometric surveillance in public spaces. Emotion recognition in workplaces. Predictive policing based on profiling. Building systems that implement these is illegal in the EU regardless of intent.

**How it works:**  
A two-stage detection pipeline:

**Stage 1 — Fast keyword pre-screen:** A list of high-precision patterns is matched against the lowercased prompt using string contains. Patterns like `"social credit score"`, `"biometric surveillance"`, `"emotion recognition workplace"` have near-zero false positive rate — they almost never appear in legitimate prompts. This stage requires no AI inference and completes in microseconds.

**Stage 2 — AI deep check:** If the fast screen passes, the prompt is sent to Groq with a system prompt containing the full text of Article 5's seven categories, instructions to distinguish implementation requests from educational questions, and a structured JSON output format. This catches nuanced and indirect requests that the keyword screen misses.

A match at either stage returns HTTP 451 Unavailable For Legal Reasons — defined by RFC 7725 specifically for content blocked due to legal obligations. The response body includes the exact EU AI Act article, a plain-English description, and the precise EUR-Lex URL for that section of the law.

**The mindset:**  
The two-stage approach is a deliberate engineering trade-off. Running an AI inference call on every request adds latency and cost. The fast pre-screen catches obvious cases instantly and only escalates ambiguous cases to the AI. This is the same pattern used in spam filters, fraud detection systems, and content moderation at scale — cheap heuristics first, expensive ML second.

**Why HTTP 451 specifically:**  
403 Forbidden means the server understood the request but refuses to authorise it. 451 Unavailable For Legal Reasons means the server cannot fulfill the request due to a legal obligation. These are semantically different. A compliance-aware client can inspect the 451 response, extract the legal citation, and surface it appropriately to the end user or log it for their own compliance records.

---

### Feature 3 — Human review queue

**The problem it solves:**  
AI classifiers are probabilistic. A score of 0.72 means the model thinks the request is probably harmful — but probably is not certainly. Auto-blocking everything above 0.4 is too aggressive and frustrates legitimate users. Auto-allowing everything below 0.75 is too permissive and misses real threats. The space between these thresholds requires human judgment.

Additionally, some categories — data extraction attempts, identity manipulation, prompt injection — warrant human review regardless of confidence score, because the stakes of a false negative are high enough that a machine should not make the final call alone.

**How it works:**  
When a request meets the review criteria, the gateway enqueues it in the `review_queue` table and begins polling for a decision. The HTTP connection is held open — the client's request is literally suspended, waiting. The poll checks the database every second.

An operator on the `/platform` admin interface sees the pending item appear in the Review Queue section (which polls the API every 3 seconds). They can read the prompt, see the AI classifier's reasoning, and click Approve or Reject. The gateway's poll detects the status change within one second, and either forwards the request to Groq (approved) or returns a 403 with the reviewer's decision recorded (rejected).

If no decision is made within 5 minutes, the item expires and the request is blocked by default. The default-block-on-timeout behaviour satisfies the EU AI Act Article 14 precautionary principle — when in doubt, block rather than allow.

**The mindset:**  
Human-in-the-loop is not a fallback for when AI fails. It is a deliberate design choice for the cases where AI should not be making the final decision. Article 14 of the EU AI Act explicitly requires human oversight for high-risk AI systems. This queue operationalises that requirement with a real workflow rather than a policy checkbox.

**Technical implementation detail:**  
The polling approach was chosen over channels or WebSockets deliberately. In a multi-instance deployment (multiple gateway replicas), an in-memory channel cannot receive a decision written by another instance. Database polling works correctly regardless of how many gateway instances are running.

---

### Feature 4 — Incident reporting with Resend email alerts

**The problem it solves:**  
A blocked request is a signal. A critical incident is a pattern. When someone attempts to use your AI gateway to design a citizen social scoring system, that is not just a blocked prompt — it is a security event that your team should know about immediately, not discover in a log review a week later.

**How it works:**  
Every high-confidence block (score ≥ 0.75), every Article 5 violation, and every human-rejected request creates a record in the `incidents` table. A severity is assigned using a deterministic mapping:

```
Article 5 violation        → critical
Unacceptable risk level    → critical
High risk + jailbreak      → high
High risk + other          → medium
Sensitive category         → medium
Identity manipulation      → low
```

For medium, high, and critical severity incidents, an email is sent asynchronously via the Resend API in a goroutine — the main request pipeline does not wait for the email to send. The email contains the severity, category, EU AI Act article, the offending prompt, and a link to the admin platform.

**The mindset:**  
Asynchronous alerting is a deliberate choice. Email delivery is not guaranteed to be fast — it can take seconds. Blocking the request-response cycle while waiting for an email would add unacceptable latency to every blocked request. By firing the email in a goroutine, the 403 response returns to the user immediately while the alert delivers in the background.

The severity grading system mirrors how security operations centres (SOCs) work — not all alerts are equal, and alert fatigue from low-severity notifications undermines the system. Only medium and above trigger emails.

---

### Feature 5 — Configurable log retention and GDPR right to erasure

**The problem it solves:**  
Storing logs forever creates legal risk. GDPR and the EU AI Act both impose obligations around data minimisation — you should not keep personal data longer than necessary. A configurable retention policy with automated purging ensures compliance without manual intervention.

The right to erasure (GDPR Article 17) gives individuals the right to request deletion of their personal data. For an AI gateway, this means all audit log entries associated with their API key.

**How it works:**  
A `retention_policy` table stores the retention period in days. A goroutine starts at launch and runs a purge 10 seconds after startup (to catch any overdue records from the previous deployment) and then every 24 hours thereafter. The purge deletes audit logs, resolved incidents, and decided review queue items older than the retention period. Unresolved incidents are never purged — they may still require investigation.

The erasure endpoint accepts an API key and deletes all `audit_logs` rows where `api_key = ?`. The operation is logged with a timestamp for the erasure event itself.

**The mindset:**  
Data retention is operationalised as code, not policy. Many organisations have a data retention policy written in a document that nobody enforces. Making it a configurable system feature that runs automatically means the policy is enforced regardless of whether anyone remembers to run it manually.

---

### Feature 6 — API key management system

**The problem it solves:**  
If your AI gateway uses a single shared API key for all users, you cannot revoke access for one user without affecting all users. You cannot attribute abuse to a specific user. You cannot set different rate limits for different customers. You cannot offer tiered access. This is not how production API products work.

**How it works:**  
The `KeyManager` generates cryptographically random 24-byte keys with a `gw_` prefix using `crypto/rand` — the same approach used by Stripe, OpenAI, and Anthropic. Keys are stored in the `api_keys` table with full lifecycle management:

- **Active:** key works, requests go through
- **Suspended:** key temporarily disabled, can be reactivated without regenerating
- **Revoked:** permanent, with a reason and timestamp recorded
- **Expired:** automatically marked when the expiry date passes

An in-memory cache maps raw key values to their records for O(1) validation on every request — database round-trip only happens on cache miss or usage update.

The key value is shown in full exactly once on generation and never again. This is a deliberate UX choice that mirrors industry practice and signals to users that they must store the key securely.

**Rate limiting per key:**  
Each key has an optional `rate_limit` field (requests per minute). The policy engine uses the key's individual limit if set, or falls back to the system default. Rate limit buckets are keyed by the raw API key value, not by IP address — this correctly handles corporate NAT and shared networks where many users share one IP.

---

## 6. The request pipeline — step by step

```
1. HTTP POST /ai arrives with X-API-Key header
   └── Method check → 405 if not POST

2. KeyManager.Validate()
   └── Cache lookup → O(1)
   └── Status check → 401 if revoked/suspended/expired
   └── Expiry check → auto-mark expired if past date
   └── Background: recordUsage() increments request_count

3. JSON body decode
   └── 400 if invalid JSON
   └── 400 if prompt is empty or whitespace only

4. Policy.Check() — rate limiting
   └── Sliding window per API key
   └── Prune timestamps older than 1 minute
   └── 429 if count >= limit

5. compliance.Detector.Check() — Article 5 prohibited use
   └── Fast keyword pre-screen (microseconds)
   └── If no match: Groq AI deep check (10s timeout)
   └── confidence >= 0.7 → 451 + incident(critical) + email

6. policy.ClassifyWithAI() — intent classification
   └── Groq Llama 3.3 70B, temperature 0.0, max_tokens 400
   └── Returns: is_harmful, category, score, reasoning_chain,
              eu_article, risk_level, indicators

7. shouldReview() check
   └── Sensitive category (data_extraction/identity_manipulation/
       prompt_injection) → review regardless of score
   └── Borderline score 0.4–0.75 → review
   └── If review needed:
       └── Enqueue in review_queue table
       └── Poll database every 1 second
       └── Approved → continue to step 8
       └── Rejected → 403 + incident + log
       └── Expired (5 min) → 403 + log

8. Auto-block check
   └── is_harmful AND score >= 0.75
   └── 403 + incident(severity based on risk/category) + email alert

9. Forward to Groq
   └── POST api.groq.com/openai/v1/chat/completions
   └── Model: llama-3.3-70b-versatile
   └── 10 second timeout
   └── 500 if Groq returns error

10. Log to audit_logs
    └── All fields including reasoning_chain, risk_level,
        eu_article, classifier_score
    └── 200 + response to user
```

**Fail-open policy:**  
If the Groq classifier returns an error or times out at step 6, the request is allowed through with a safe default classification. Availability is prioritised over the classifier. A down classifier should not take down the gateway — legitimate users would be incorrectly blocked.

---

## 7. Security design

### Authentication layers

**Layer 1 — User authentication (X-API-Key)**  
Every request to `/ai` must include a `gw_` prefixed API key in the `X-API-Key` header. The key is validated against the in-memory cache in O(1) time. Invalid, revoked, suspended, or expired keys return 401 immediately.

**Layer 2 — Admin authentication (HTTP Basic Auth)**  
Every admin route (`/platform`, `/admin/*`) is wrapped with the `AdminAuth` middleware. Credentials are validated using `crypto/subtle.ConstantTimeCompare` — a function that always takes the same amount of time regardless of where in the string comparison fails. This prevents timing attacks where an attacker could determine the correct username character-by-character by measuring response times.

### Credential isolation

The Groq API key never leaves the server. It is stored as an environment variable, used server-side to forward requests, and never included in any response to any client. The browser's network inspector will show requests to the gateway's domain only — never to `api.groq.com`.

### Non-root container

The Docker container runs as user `nobody` (uid 65534). This means that if an attacker achieves remote code execution through a vulnerability in the application, they have no filesystem write permissions and cannot install persistence mechanisms or read sensitive files outside the `/app` directory.

### No sensitive data in logs

The gateway logs request metadata (IP, status, risk level) but never logs the full Groq API key, the admin password, or the raw API key value beyond its first 10 characters. The audit log stores the prompt and response, which is necessary for compliance, but this data is subject to the configurable retention policy and right-to-erasure.

---

## 8. DevOps and CI/CD

### The pipeline

```yaml
on: push to main

jobs:
  test:   go vet + go test -race + coverage
  build:  CGO_ENABLED=0 binary → artifact upload
  docker: image build + Trivy CVE scan → report upload
  lint:   golangci-lint (non-blocking, advisory only)
```

The critical path is `test → build → docker`. Lint runs in parallel and never blocks a deployment. This is a deliberate policy decision: lint issues are code quality feedback, not deployment blockers. A lint warning should not prevent a security fix from reaching production.

### Race condition detection

`go test -race` instruments all memory accesses and detects data races at runtime. This is critical for the gateway which has multiple concurrent goroutines:

- The rate limiter's bucket map is accessed concurrently by all requests
- The key manager's in-memory cache is read by every request and written by key lifecycle operations
- The review queue's expire loop runs in a goroutine while Poll() runs in the request goroutine
- The incident manager fires email alerts in goroutines while the main pipeline continues

Every shared data structure uses `sync.Mutex` or `sync.RWMutex` correctly, verified by the race detector on every push.

### Graceful shutdown

When Render sends SIGTERM before restarting a container (during deploys, scaling, or maintenance), the gateway:

1. Stops accepting new connections immediately
2. Waits up to 30 seconds for in-flight requests to complete
3. Closes the database connection cleanly
4. Exits with code 0

Without this, a deploy that happens while a user is waiting in the review queue (which holds the connection open for up to 5 minutes) would hard-kill their request with no response. With graceful shutdown, the connection is maintained until the review completes or the 30-second drain window expires.

### Infrastructure as code

`render.yaml` defines the complete deployment configuration in the repository. Runtime (Docker), Dockerfile path, health check path, and all environment variable keys are version-controlled. This means:

- The deployment is reproducible — fork the repo and connect to Render, same configuration
- Changes to deployment configuration go through code review
- Rolling back a deployment configuration is a git revert

---

## 9. Challenges and how I solved them

### Challenge 1 — Getting the AI classifier to return structured JSON reliably

**The problem:**  
Large language models are probabilistic. When asked to return JSON, they sometimes return it wrapped in markdown code fences (` ```json `), sometimes with preamble text, sometimes with trailing commentary. A JSON unmarshal error means the classification fails and the request falls through to the fail-open default.

**What I tried:**  
Initially I used `json.Unmarshal` directly on the model's response. This failed approximately 15-20% of the time due to formatting variations.

**How I solved it:**  
A multi-step cleaning pipeline:
1. Trim leading/trailing whitespace
2. Strip ` ```json ` prefix and ` ``` ` suffix
3. Find the first `{` and last `}` using `strings.Index` and `strings.LastIndex`
4. Extract only the substring between those indices
5. Attempt `json.Unmarshal` on the cleaned content

This handles all observed formatting variations. The system prompt is also explicit: "Return ONLY raw JSON. No markdown. No code fences. Nothing outside the JSON object." The combination of explicit instruction and defensive parsing gives near-100% parse reliability.

**What I learned:**  
Prompt engineering and defensive code are not alternatives — they are complements. Write the best prompt you can, then also write code that handles the cases where the prompt is not followed perfectly.

---

### Challenge 2 — The human review queue race condition

**The problem:**  
The review queue works by holding an HTTP connection open while polling a database for a status change. The Poll() function runs in the HTTP handler's goroutine. The Decide() function (called by the admin's approve/reject action) runs in a different goroutine. The expireLoop() runs in yet another goroutine. Three goroutines reading and writing the same database rows.

**What I tried:**  
Initially I used an in-memory channel — Poll() would block on a channel receive, and Decide() would send on the channel. This worked in development but would break in a multi-instance deployment where Decide() might run on a different server instance than Poll().

**How I solved it:**  
Moved to pure database polling. Poll() queries the database every second and checks the `status` field. Decide() writes to the database. The database is the shared state. This works correctly regardless of how many instances are running, and SQLite's write serialisation prevents concurrent write conflicts.

The 1-second polling interval means a human's approve/reject action is reflected within 1 second — fast enough to feel responsive in the UI, slow enough to not hammer the database.

**What I learned:**  
For distributed systems, database-as-coordination is often the right choice over in-memory coordination. In-memory is faster but breaks the moment you have more than one process. Database is slower but always correct.

---

### Challenge 3 — The golangci-lint Go version incompatibility in CI

**The problem:**  
The project was initially set to Go 1.26 in `go.mod`. The golangci-lint binary in CI is compiled with Go 1.24 and refuses to lint modules targeting a newer Go version, producing `error: the Go language version (go1.24) used to build golangci-lint is lower than the targeted Go version (1.26.4)`.

**What I tried:**  
Pinning a specific golangci-lint version, using `--no-config` to bypass the config file, and setting `GO_VERSION: "1.26"` in the CI environment. None of these resolved the fundamental incompatibility between the linter binary's Go version and the module's Go directive.

**How I solved it:**  
Downgraded the `go` directive in `go.mod` from `1.26` to `1.24`. The application code does not use any Go 1.25 or 1.26 specific language features — the upgrade was unnecessary. Aligning `go.mod`, the CI `GO_VERSION` environment variable, the Dockerfile base image, and the golangci-lint version to all use Go 1.24 resolved the incompatibility entirely.

Additionally, moved lint to `continue-on-error: true` so lint issues are advisory warnings rather than deployment blockers. The critical pipeline (test → build → docker) is never blocked by lint.

**What I learned:**  
Go version alignment across all tooling is not optional. The module directive, the CI Go version, the Docker base image, and the linter must all agree. When they do not, the errors are cryptic and the fixes are counterintuitive.

---

### Challenge 4 — Render's automatic Go buildpack detection

**The problem:**  
Render auto-detected the repository as a Go project and used its own Go buildpack instead of the Docker runtime. The buildpack compiled the binary correctly but did not set execute permissions, resulting in `bash: ./app: Permission denied` (exit code 126) on every deploy.

**What I tried:**  
Adding `chmod +x` to the binary in the Dockerfile, using `ENTRYPOINT` instead of `CMD`, and setting explicit `--chown` on the COPY instruction. None of these mattered because Render was not using the Dockerfile at all.

**How I solved it:**  
The service needed to be deleted and recreated with Docker explicitly selected as the runtime. Render's auto-detection of Go overrode the `render.yaml` `runtime: docker` setting for services that already existed as Go services. Creating a new service from scratch with Docker selected in the initial configuration dialog allowed Render to respect the Dockerfile.

**What I learned:**  
Infrastructure-as-code tools like `render.yaml` are respected at creation time. Changing the runtime of an existing service sometimes requires deleting and recreating it rather than modifying configuration. This is a Render-specific behaviour but the general lesson is that IaC tools have different behaviour for create vs update operations.

---

### Challenge 5 — SQLite and the ephemeral filesystem on Render free tier

**The problem:**  
Render's free tier does not provide persistent disk storage. The SQLite database at `/tmp/gateway.db` is wiped on every deploy. This means generated API keys, audit logs, and incidents are lost on every redeploy.

**What I tried:**  
Using an in-memory logger as a fallback, which works but obviously does not persist between restarts either.

**How I solved it (partially):**  
Two workarounds for the immediate problem:

1. The `GATEWAY_API_KEYS` environment variable allows static keys to be loaded from configuration on startup — these survive redeploys because they live in the environment, not the database.

2. The database migration runs on every startup and recreates tables if they do not exist — so the application always starts in a valid state even with an empty database.

The real solution is PostgreSQL, which is documented as the production migration path. Render offers a free managed PostgreSQL database for 90 days. The migration requires changing the SQLite driver to `lib/pq` and updating the SQL dialect (PostgreSQL uses `$1` placeholders instead of `?`).

**What I learned:**  
Ephemeral filesystems on PaaS platforms are a fundamental constraint, not a bug. Stateless application design — where all persistent state lives in a separate database service — is the correct architecture for containerised deployments. SQLite is appropriate for development and single-server deployments with persistent disk, not for PaaS free tiers.

---

## 10. What I learned

**AI classification is an engineering problem, not just a prompt engineering problem.**  
Getting an LLM to reliably classify prompts and return structured data requires careful system prompt design, defensive response parsing, explicit temperature settings, and fallback behaviour for classifier failures. The classifier is called on every single request — it must be fast, reliable, and gracefully degradable.

**HTTP status codes have semantic meaning that matters.**  
Using 451 instead of 403 for legally prohibited content is not pedantry — it is the difference between a response that correctly communicates legal obligation and one that incorrectly implies authorisation failure. Clients, proxies, and compliance systems that understand HTTP semantics will treat these differently.

**Concurrency bugs are silent until they are catastrophic.**  
The rate limiter's map, the key manager's cache, and the review queue's polling loop all involved concurrent access patterns that look correct in single-threaded reasoning but race in production. `go test -race` caught several of these before they reached deployment.

**The gap between a working prototype and a production system is mostly operational concerns.**  
The core AI classification logic took a day to write. The audit logging, key management, incident reporting, retention policy, graceful shutdown, Docker hardening, and CI pipeline took the rest of the time. This is accurate to how real systems work — the feature is 20% of the work, the operability is 80%.

**Compliance requirements are engineering requirements.**  
The EU AI Act is not a legal document that sits in a filing cabinet. Every article in it translates to a specific technical feature: Article 5 is the prohibited use detector, Article 13 is the reasoning chain audit log, Article 14 is the human review queue, Article 17 is the retention policy and erasure endpoint. Reading the regulation and translating it into code is a skill, and it produces better systems than trying to retrofit compliance onto an existing design.

---

## 11. Why this matters in the real world

The EU AI Act entered into force in August 2024. The prohibited practices in Article 5 became enforceable in February 2025. The high-risk AI system requirements follow in 2026. Any company deploying AI in the EU — which includes any company with EU customers — is subject to these requirements.

The fines are not theoretical: up to 35 million euros or 7% of worldwide annual turnover for Article 5 violations. For a company with 1 billion euros in revenue, that is 70 million euros per violation.

Every company integrating AI into a product right now needs to answer these questions:

- Can you produce an audit trail for every AI decision you made in the last 90 days?
- Can you demonstrate that you blocked prohibited use cases under Article 5?
- Can you show that borderline decisions went through human review?
- Can you delete all AI interaction data for a specific user on request?

Most companies cannot. They have a direct integration to an AI API, no structured logging, no governance layer, and no human oversight mechanism. This gateway is a blueprint for what that governance layer looks like.

Beyond compliance, there is a product reason. An AI gateway that can issue, revoke, and rate-limit API keys per customer is the foundation of an AI API business. Every company that sells AI API access — OpenAI, Anthropic, Cohere, AI21 — has a system that does exactly what this gateway does. The governance and key management features are not extras — they are the business model.

---

*This documentation covers the complete technical design, implementation decisions, and operational lessons from building the AI Gateway. For setup instructions and API reference, see [README.md](./README.md).*