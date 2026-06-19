package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"ai-gateway/internal/auth"
	"ai-gateway/internal/compliance"
	"ai-gateway/internal/logger"
	"ai-gateway/internal/metrics"
	"ai-gateway/internal/policy"
)

// Gateway is the core request handler
type Gateway struct {
	Name             string
	keyManager       *auth.KeyManager
	authLayer        *auth.Auth
	policy           *policy.Engine
	prohibited       *compliance.Detector
	reviewQueue      *compliance.ReviewQueue
	incidentManager  *compliance.IncidentManager
	retentionManager *compliance.RetentionManager
	logger           *logger.Logger
	apiKey           string
}

// New wires together all gateway dependencies
func New(l *logger.Logger) *Gateway {
	if err := godotenv.Load(); err != nil {
		fmt.Println("[Gateway] No .env file — reading from environment directly")
	}

	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		fmt.Println("[Warning] GROQ_API_KEY not set — AI calls will fail")
	} else {
		fmt.Println("[Gateway] Groq API key loaded")
	}

	resendKey := os.Getenv("RESEND_API_KEY")
	emailTo := os.Getenv("ALERT_EMAIL_TO")
	emailFrom := os.Getenv("ALERT_EMAIL_FROM")
	if emailFrom == "" {
		emailFrom = "onboarding@resend.dev"
	}

	db := l.DB()
	km := auth.NewKeyManager(db)

	staticKeys := os.Getenv("GATEWAY_API_KEYS")
	if staticKeys != "" {
		for _, k := range strings.Split(staticKeys, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				_, _ = km.Generate(k, "env-configured", 5, nil)
			}
		}
	}

	return &Gateway{
		Name:             "AI Gateway v1",
		keyManager:       km,
		authLayer:        auth.NewAuth(km),
		policy:           policy.New(db, apiKey),
		prohibited:       compliance.NewDetector(apiKey),
		reviewQueue:      compliance.NewReviewQueue(db),
		incidentManager:  compliance.NewIncidentManager(db, resendKey, emailTo, emailFrom),
		retentionManager: compliance.NewRetentionManager(db),
		logger:           l,
		apiKey:           apiKey,
	}
}

// reviewDecision encapsulates why a request was routed to human review
type reviewDecision struct {
	needed bool
	reason string
}

func shouldReview(result policy.ClassifyResult) reviewDecision {
	sensitiveCategories := map[string]bool{
		"data_extraction":       true,
		"identity_manipulation": true,
		"prompt_injection":      true,
	}
	if sensitiveCategories[result.Category] {
		return reviewDecision{true, fmt.Sprintf(
			"sensitive category %q requires human review (score: %.2f)", result.Category, result.Score)}
	}
	if result.IsHarmful && result.Score >= 0.4 && result.Score < 0.75 {
		return reviewDecision{true, fmt.Sprintf(
			"borderline score %.2f — human review required", result.Score)}
	}
	return reviewDecision{needed: false}
}

// HandleAI is the main gateway endpoint — full EU AI Act compliant pipeline
func (g *Gateway) HandleAI(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "only POST requests are accepted"})
		return
	}

	// ── Step 1: Authentication ────────────────────────────────────────────
	valid, keyRecord, reason := g.authLayer.ValidateFull(r)
	if !valid {
		fmt.Printf("[Auth] Rejected — %s\n", reason)
		metrics.RequestsTotal.WithLabelValues("unauthorized").Inc()
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"status": "unauthorized", "reason": reason})
		return
	}
	fmt.Printf("[Auth] Accepted — key: %s (%s)\n", keyRecord.Name, keyRecord.Owner)

	// ── Step 2: Parse request body ────────────────────────────────────────
	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		metrics.RequestsTotal.WithLabelValues("error").Inc()
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "invalid JSON body"})
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		metrics.RequestsTotal.WithLabelValues("error").Inc()
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "prompt cannot be empty"})
		return
	}

	prompt := strings.TrimSpace(body.Prompt)
	clientIP := r.RemoteAddr
	rawKey := r.Header.Get("X-API-Key")

	// ── Step 3: Per-key rate limiting ─────────────────────────────────────
	if allowed, limitReason := g.policy.Check(rawKey, prompt, keyRecord.RateLimit); !allowed {
		fmt.Printf("[Policy] Rate limited — key: %s\n", keyRecord.Name)
		metrics.RequestsTotal.WithLabelValues("rate_limited").Inc()
		metrics.RateLimitHits.WithLabelValues(keyRecord.Name).Inc()
		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "blocked",
			Blocked: true, Reason: limitReason, RiskLevel: logger.RiskLimited,
		})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"status": "blocked", "reason": limitReason})
		return
	}

	// ── Step 4: Article 5 prohibited use detector ─────────────────────────
	fmt.Println("[Prohibited] Checking Article 5 categories")
	prohibitedResult := g.prohibited.Check(prompt)

	if prohibitedResult.IsProhibited && prohibitedResult.Confidence >= 0.7 {
		reason := fmt.Sprintf("EU AI Act %s violation — %s: %s",
			prohibitedResult.Article, prohibitedResult.ArticleDescription, prohibitedResult.Reason)

		metrics.RequestsTotal.WithLabelValues("blocked").Inc()
		metrics.BlockedByCategory.WithLabelValues("prohibited_" + prohibitedResult.Category).Inc()
		metrics.ProhibitedUseDetected.WithLabelValues(prohibitedResult.Article).Inc()
		metrics.RequestsByRiskLevel.WithLabelValues("unacceptable").Inc()
		metrics.RequestDuration.WithLabelValues("blocked").Observe(time.Since(start).Seconds())

		g.keyManager.RecordBlock(rawKey)
		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "blocked", Blocked: true,
			Reason: reason, ReasoningChain: "Article 5 — " + prohibitedResult.Reason,
			RiskLevel: logger.RiskUnacceptable, EUArticle: prohibitedResult.Article,
			Category: prohibitedResult.Category, ClassifierScore: prohibitedResult.Confidence,
		})
		go g.incidentManager.Create(0, compliance.SeverityCritical,
			prohibitedResult.Category, prohibitedResult.Article, prompt, clientIP, reason)

		w.WriteHeader(http.StatusUnavailableForLegalReasons)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "unavailable_for_legal_reasons", "reason": reason,
			"article": prohibitedResult.Article, "article_description": prohibitedResult.ArticleDescription,
			"category": prohibitedResult.Category, "confidence": prohibitedResult.Confidence,
			"indicators": prohibitedResult.Indicators, "legal_reference": prohibitedResult.LegalReference,
			"risk_level": "unacceptable",
		})
		return
	}

	// ── Step 5: AI intent classifier ─────────────────────────────────────
	fmt.Println("[Policy] Running AI intent classifier")
	classifyStart := time.Now()
	classifyResult := g.policy.ClassifyWithAI(prompt)
	metrics.ClassifierDuration.Observe(time.Since(classifyStart).Seconds())

	riskLevel := mapRiskLevel(classifyResult.RiskLevel)
	metrics.RequestsByRiskLevel.WithLabelValues(classifyResult.RiskLevel).Inc()

	refURL, refDesc := compliance.ArticleReference(classifyResult.EUArticle)

	// ── Step 5a: Review routing ───────────────────────────────────────────
	review := shouldReview(classifyResult)
	if review.needed {
		fmt.Printf("[ReviewQueue] Routing — %s\n", review.reason)
		metrics.ReviewQueuePending.Inc()
		reviewStart := time.Now()

		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "review_pending",
			Reason: review.reason, ReasoningChain: classifyResult.ReasoningChain,
			RiskLevel: riskLevel, EUArticle: classifyResult.EUArticle,
			Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
		})

		itemID, err := g.reviewQueue.Enqueue(0, prompt, clientIP,
			classifyResult.Category, classifyResult.ReasoningChain, classifyResult.Score)
		if err != nil {
			metrics.ReviewQueuePending.Dec()
			metrics.RequestsTotal.WithLabelValues("error").Inc()
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "review queue unavailable"})
			return
		}

		decision := g.reviewQueue.Poll(itemID)
		metrics.ReviewQueuePending.Dec()
		metrics.ReviewQueueWaitDuration.Observe(time.Since(reviewStart).Seconds())

		switch decision {
		case compliance.ReviewApproved:
			fmt.Printf("[ReviewQueue] Approved (id:%d)\n", itemID)
			metrics.RequestsTotal.WithLabelValues("review_approved").Inc()

		case compliance.ReviewRejected:
			metrics.RequestsTotal.WithLabelValues("blocked").Inc()
			metrics.BlockedByCategory.WithLabelValues(classifyResult.Category).Inc()
			metrics.RequestDuration.WithLabelValues("blocked").Observe(time.Since(start).Seconds())
			g.keyManager.RecordBlock(rawKey)
			g.writeLog(logger.LogEntry{
				ClientIP: clientIP, Prompt: prompt, Status: "blocked", Blocked: true,
				Reason: "rejected by human reviewer", RiskLevel: riskLevel,
				Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
			})
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "blocked", "reason": "rejected by human reviewer", "review_id": itemID,
			})
			return

		default:
			metrics.RequestsTotal.WithLabelValues("blocked").Inc()
			metrics.RequestDuration.WithLabelValues("blocked").Observe(time.Since(start).Seconds())
			g.writeLog(logger.LogEntry{
				ClientIP: clientIP, Prompt: prompt, Status: "blocked", Blocked: true,
				Reason: "review timed out — blocked by default (Article 14)", RiskLevel: riskLevel,
				Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
			})
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "blocked", "reason": "human review timed out — blocked per EU AI Act Article 14", "review_id": itemID,
			})
			return
		}
	}

	// ── Step 5b: Auto-block ───────────────────────────────────────────────
	if classifyResult.IsHarmful && classifyResult.Score >= 0.75 {
		reason := fmt.Sprintf("[%s] %s (%.0f%% confidence)",
			classifyResult.Category, classifyResult.Reason, classifyResult.Score*100)

		metrics.RequestsTotal.WithLabelValues("blocked").Inc()
		metrics.BlockedByCategory.WithLabelValues(classifyResult.Category).Inc()
		metrics.RequestDuration.WithLabelValues("blocked").Observe(time.Since(start).Seconds())

		g.keyManager.RecordBlock(rawKey)
		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "blocked", Blocked: true,
			Reason: reason, ReasoningChain: classifyResult.ReasoningChain,
			RiskLevel: riskLevel, EUArticle: classifyResult.EUArticle,
			Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
		})
		severity := compliance.DetermineSeverity(classifyResult.RiskLevel, classifyResult.Category, classifyResult.EUArticle)
		go g.incidentManager.Create(0, severity, classifyResult.Category, classifyResult.EUArticle, prompt, clientIP, reason)

		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "blocked", "reason": reason,
			"category": classifyResult.Category, "score": classifyResult.Score,
			"risk_level": classifyResult.RiskLevel, "eu_article": classifyResult.EUArticle,
			"article_description": refDesc, "legal_reference": refURL,
			"reasoning_chain": classifyResult.ReasoningChain, "indicators": classifyResult.Indicators,
		})
		return
	}

	// ── Step 6: Forward to Groq ───────────────────────────────────────────
	fmt.Printf("[Gateway] Forwarding — key: %s\n", keyRecord.Name)
	groqStart := time.Now()
	response, err := g.callAI(prompt)
	metrics.GroqDuration.Observe(time.Since(groqStart).Seconds())

	if err != nil {
		metrics.RequestsTotal.WithLabelValues("error").Inc()
		metrics.RequestDuration.WithLabelValues("error").Observe(time.Since(start).Seconds())
		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "error",
			Reason: err.Error(), RiskLevel: riskLevel,
			Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
		})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "AI service error: " + err.Error()})
		return
	}

	metrics.RequestsTotal.WithLabelValues("allowed").Inc()
	metrics.RequestDuration.WithLabelValues("allowed").Observe(time.Since(start).Seconds())

	g.writeLog(logger.LogEntry{
		ClientIP: clientIP, Prompt: prompt, Response: response, Status: "allowed",
		ReasoningChain: classifyResult.ReasoningChain, RiskLevel: riskLevel,
		Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success", "prompt": prompt, "response": response, "risk_level": classifyResult.RiskLevel,
	})
}

// HandleKeys manages API keys
func (g *Gateway) HandleKeys(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/keys"), "/")

	switch {
	case r.Method == http.MethodGet && path == "":
		status := r.URL.Query().Get("status")
		keys, err := g.keyManager.GetAll(status)
		if err != nil {
			keys = []auth.APIKey{}
		}
		if keys == nil {
			keys = []auth.APIKey{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "count": len(keys), "keys": keys})

	case r.Method == http.MethodGet && path == "stats":
		stats := g.keyManager.Stats()
		metrics.ActiveAPIKeys.Set(float64(stats["active"]))
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "stats": stats})

	case r.Method == http.MethodPost && path == "generate":
		var body struct {
			Name        string `json:"name"`
			Owner       string `json:"owner"`
			RateLimit   int    `json:"rate_limit"`
			ExpiresDays int    `json:"expires_days"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "invalid JSON"})
			return
		}
		if body.Name == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "name is required"})
			return
		}
		var expiresAt *time.Time
		if body.ExpiresDays > 0 {
			t := time.Now().AddDate(0, 0, body.ExpiresDays)
			expiresAt = &t
		}
		key, err := g.keyManager.Generate(body.Name, body.Owner, body.RateLimit, expiresAt)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		metrics.ActiveAPIKeys.Inc()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "created",
			"message": "Save this key — it will never be shown in full again",
			"key":     key,
		})

	case r.Method == http.MethodPost && path == "revoke":
		var body struct {
			KeyOrID   string `json:"key_or_id"`
			RevokedBy string `json:"revoked_by"`
			Reason    string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.KeyOrID == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "key_or_id required"})
			return
		}
		if err := g.keyManager.Revoke(body.KeyOrID, body.RevokedBy, body.Reason); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		metrics.ActiveAPIKeys.Dec()
		json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})

	case r.Method == http.MethodPost && path == "suspend":
		var body struct {
			ID int    `json:"id"`
			By string `json:"by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "id required"})
			return
		}
		if err := g.keyManager.Suspend(body.ID, body.By); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "suspended"})

	case r.Method == http.MethodPost && path == "activate":
		var body struct {
			ID int    `json:"id"`
			By string `json:"by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "id required"})
			return
		}
		if err := g.keyManager.Activate(body.ID, body.By); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "activated"})

	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "unknown route"})
	}
}

// HandleRetention manages log retention policy
func (g *Gateway) HandleRetention(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/retention"), "/")

	switch {
	case r.Method == http.MethodGet && path == "":
		p, err := g.retentionManager.GetPolicy()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "policy": p, "storage": g.retentionManager.StorageStats()})

	case r.Method == http.MethodPost && path == "":
		var body struct {
			Days      int    `json:"days"`
			UpdatedBy string `json:"updated_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Days == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "days required"})
			return
		}
		updated, err := g.retentionManager.UpdatePolicy(body.Days, body.UpdatedBy)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "updated", "policy": updated,
			"message": fmt.Sprintf("Logs older than %d days will be purged nightly", body.Days),
		})

	case r.Method == http.MethodPost && path == "purge":
		result, err := g.retentionManager.Purge()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "purged", "result": result})

	case r.Method == http.MethodDelete && path == "erase":
		var body struct {
			APIKey string `json:"api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.APIKey == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "api_key required"})
			return
		}
		result, err := g.retentionManager.EraseByAPIKey(body.APIKey)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "erased", "result": result})

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// HandleIncidents manages security incidents
func (g *Gateway) HandleIncidents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/incidents"), "/")

	switch {
	case r.Method == http.MethodGet && path == "":
		incidents, err := g.incidentManager.GetAll(r.URL.Query().Get("severity"))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if incidents == nil {
			incidents = []compliance.Incident{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "count": len(incidents), "incidents": incidents})

	case r.Method == http.MethodGet && path == "stats":
		stats := g.incidentManager.Stats()
		if unresolved, ok := stats["unresolved"].(int); ok {
			metrics.IncidentsUnresolved.Set(float64(unresolved))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "stats": stats})

	case r.Method == http.MethodPost && path == "resolve":
		var body struct {
			ID         int    `json:"id"`
			ResolvedBy string `json:"resolved_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body.ResolvedBy == "" {
			body.ResolvedBy = "admin"
		}
		if err := g.incidentManager.Resolve(body.ID, body.ResolvedBy); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		metrics.IncidentsUnresolved.Dec()
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "resolved", "id": body.ID})

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// HandleReview manages the human review queue
func (g *Gateway) HandleReview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/review"), "/")

	switch {
	case r.Method == http.MethodGet && path == "":
		items, err := g.reviewQueue.GetPending()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if items == nil {
			items = []compliance.ReviewItem{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "count": len(items), "items": items})

	case r.Method == http.MethodGet && path == "all":
		items, err := g.reviewQueue.GetAll()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if items == nil {
			items = []compliance.ReviewItem{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "count": len(items), "items": items})

	case r.Method == http.MethodGet && path == "stats":
		stats := g.reviewQueue.Stats()
		metrics.ReviewQueuePending.Set(float64(stats["pending"]))
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "stats": stats})

	case r.Method == http.MethodPost && path == "approve":
		g.handleDecision(w, r, compliance.ReviewApproved)

	case r.Method == http.MethodPost && path == "reject":
		g.handleDecision(w, r, compliance.ReviewRejected)

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (g *Gateway) handleDecision(w http.ResponseWriter, r *http.Request, status compliance.ReviewStatus) {
	var body struct {
		ID       int    `json:"id"`
		Reviewer string `json:"reviewer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "id required"})
		return
	}
	if body.Reviewer == "" {
		body.Reviewer = "admin"
	}
	if err := g.reviewQueue.Decide(body.ID, status, body.Reviewer); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "decision": string(status), "id": body.ID})
}

// HandleRules manages blocked keyword rules
func (g *Gateway) HandleRules(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		rules, err := g.policy.GetRules()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "count": len(rules), "rules": rules})
	case http.MethodPost:
		var body struct {
			Word string `json:"word"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Word == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := g.policy.AddRule(body.Word); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "added", "word": body.Word})
	case http.MethodDelete:
		var body struct {
			Word string `json:"word"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Word == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := g.policy.RemoveRule(body.Word); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "removed", "word": body.Word})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// HandleAuditDetail returns full audit record for a single request ID
func (g *Gateway) HandleAuditDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var id int
	_, _ = fmt.Sscanf(parts[2], "%d", &id)
	if id == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	entry, err := g.logger.GetByID(id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	refURL, refDesc := compliance.ArticleReference(entry.EUArticle)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"entry": map[string]interface{}{
			"id": entry.ID, "timestamp": entry.Timestamp.Format("2006-01-02 15:04:05"),
			"client_ip": entry.ClientIP, "prompt": entry.Prompt, "response": entry.Response,
			"status": entry.Status, "blocked": entry.Blocked, "reason": entry.Reason,
			"reasoning_chain": entry.ReasoningChain, "risk_level": string(entry.RiskLevel),
			"eu_article": entry.EUArticle, "article_description": refDesc,
			"legal_reference": refURL, "category": entry.Category,
			"classifier_score": entry.ClassifierScore,
		},
	})
}

func (g *Gateway) writeLog(entry logger.LogEntry) {
	if g.logger == nil {
		return
	}
	if err := g.logger.Log(entry); err != nil {
		fmt.Printf("[Gateway] Log write failed: %v\n", err)
	}
}

func mapRiskLevel(level string) logger.RiskLevel {
	switch strings.ToLower(level) {
	case "unacceptable":
		return logger.RiskUnacceptable
	case "high":
		return logger.RiskHigh
	case "limited":
		return logger.RiskLimited
	default:
		return logger.RiskMinimal
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
