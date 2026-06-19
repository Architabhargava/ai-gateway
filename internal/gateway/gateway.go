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

	// Seed any static keys from env for backward compatibility
	// These are in addition to dynamically managed keys
	staticKeys := os.Getenv("GATEWAY_API_KEYS")
	if staticKeys != "" {
		for _, k := range strings.Split(staticKeys, ",") {
			k = strings.TrimSpace(k)
			if k != "" {
				// Check if already exists
				existing, _ := km.GetAll("")
				found := false
				for _, e := range existing {
					// Compare unmasked — we need to check the raw key
					// For env keys we just ensure they exist
					_ = e
					found = false // Will re-check below
					break
				}
				if !found {
					// Try to insert — will fail silently if duplicate
					_, _ = km.Generate(k, "env-configured", 5, nil)
				}
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
		return reviewDecision{true, fmt.Sprintf("sensitive category %q requires human review (score: %.2f)", result.Category, result.Score)}
	}
	if result.IsHarmful && result.Score >= 0.4 && result.Score < 0.75 {
		return reviewDecision{true, fmt.Sprintf("borderline score %.2f — human review required", result.Score)}
	}
	return reviewDecision{needed: false}
}

// HandleAI is the main gateway endpoint
func (g *Gateway) HandleAI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "only POST requests are accepted"})
		return
	}

	// ── Step 1: Authentication via KeyManager ─────────────────────────────────
	valid, keyRecord, reason := g.authLayer.ValidateFull(r)
	if !valid {
		fmt.Printf("[Auth] Rejected — %s\n", reason)
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"status": "unauthorized", "reason": reason})
		return
	}
	fmt.Printf("[Auth] Accepted — key: %s (%s)\n", keyRecord.Name, keyRecord.Owner)

	// ── Step 2: Parse request body ────────────────────────────────────────────
	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "invalid JSON body"})
		return
	}
	if strings.TrimSpace(body.Prompt) == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "prompt cannot be empty"})
		return
	}

	prompt := strings.TrimSpace(body.Prompt)
	clientIP := r.RemoteAddr

	// ── Step 3: Per-key rate limiting ─────────────────────────────────────────
	// Use the key's individual rate limit if set, otherwise system default
	// Rate limit bucket is keyed by the raw API key so each customer
	// has their own independent rate limit counter
	rawKey := r.Header.Get("X-API-Key")
	if allowed, reason := g.policy.Check(rawKey, prompt, keyRecord.RateLimit); !allowed {
		fmt.Printf("[Policy] Rate limited — key: %s (%s)\n", keyRecord.Name, rawKey[:min(10, len(rawKey))])
		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "blocked",
			Blocked: true, Reason: reason, RiskLevel: logger.RiskLimited,
		})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"status": "blocked", "reason": reason})
		return
	}

	// ── Step 4: Article 5 prohibited use detector ─────────────────────────────
	fmt.Println("[Prohibited] Checking Article 5 categories")
	prohibitedResult := g.prohibited.Check(prompt)

	if prohibitedResult.IsProhibited && prohibitedResult.Confidence >= 0.7 {
		reason := fmt.Sprintf("EU AI Act %s violation — %s: %s",
			prohibitedResult.Article, prohibitedResult.ArticleDescription, prohibitedResult.Reason)

		g.keyManager.RecordBlock(rawKey)
		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "blocked", Blocked: true,
			Reason: reason, ReasoningChain: "Article 5 — " + prohibitedResult.Reason,
			RiskLevel: logger.RiskUnacceptable, EUArticle: prohibitedResult.Article,
			Category: prohibitedResult.Category, ClassifierScore: prohibitedResult.Confidence,
		})
		go g.incidentManager.Create(0, compliance.SeverityCritical, //nolint:errcheck
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

	// ── Step 5: AI intent classifier ─────────────────────────────────────────
	fmt.Println("[Policy] Running AI intent classifier")
	classifyResult := g.policy.ClassifyWithAI(prompt)
	riskLevel := mapRiskLevel(classifyResult.RiskLevel)
	refURL, refDesc := compliance.ArticleReference(classifyResult.EUArticle)

	// ── Step 5a: Review routing ───────────────────────────────────────────────
	review := shouldReview(classifyResult)
	if review.needed {
		fmt.Printf("[ReviewQueue] Routing — %s\n", review.reason)
		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "review_pending",
			Reason: review.reason, ReasoningChain: classifyResult.ReasoningChain,
			RiskLevel: riskLevel, EUArticle: classifyResult.EUArticle,
			Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
		})

		itemID, err := g.reviewQueue.Enqueue(0, prompt, clientIP,
			classifyResult.Category, classifyResult.ReasoningChain, classifyResult.Score)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "review queue unavailable"})
			return
		}

		switch g.reviewQueue.Poll(itemID) {
		case compliance.ReviewApproved:
			fmt.Printf("[ReviewQueue] Approved (id:%d)\n", itemID)
		case compliance.ReviewRejected:
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

	// ── Step 5b: Auto-block high confidence ───────────────────────────────────
	if classifyResult.IsHarmful && classifyResult.Score >= 0.75 {
		reason := fmt.Sprintf("[%s] %s (%.0f%% confidence)", classifyResult.Category, classifyResult.Reason, classifyResult.Score*100)
		g.keyManager.RecordBlock(rawKey)
		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "blocked", Blocked: true,
			Reason: reason, ReasoningChain: classifyResult.ReasoningChain,
			RiskLevel: riskLevel, EUArticle: classifyResult.EUArticle,
			Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
		})
		severity := compliance.DetermineSeverity(classifyResult.RiskLevel, classifyResult.Category, classifyResult.EUArticle)
		go g.incidentManager.Create(0, severity, classifyResult.Category, classifyResult.EUArticle, prompt, clientIP, reason) //nolint:errcheck

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

	// ── Step 6: Forward to Groq ───────────────────────────────────────────────
	fmt.Printf("[Gateway] Forwarding — key: %s prompt: %q\n", keyRecord.Name, prompt)
	response, err := g.callAI(prompt)
	if err != nil {
		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "error",
			Reason: err.Error(), RiskLevel: riskLevel,
			Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
		})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "AI service error: " + err.Error()})
		return
	}

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

// HandleKeys manages API keys — the customer key management system
//
//	GET    /admin/keys              — list all keys (optional ?status=active)
//	GET    /admin/keys/stats        — key statistics
//	POST   /admin/keys/generate     — create a new key {name, owner, rate_limit, expires_days}
//	POST   /admin/keys/revoke       — revoke a key {key_or_id, revoked_by, reason}
//	POST   /admin/keys/suspend      — suspend a key {id}
//	POST   /admin/keys/activate     — re-activate a suspended key {id}
func (g *Gateway) HandleKeys(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/admin/keys")
	path = strings.Trim(path, "/")

	switch {

	case r.Method == http.MethodGet && path == "":
		status := r.URL.Query().Get("status")
		keys, err := g.keyManager.GetAll(status)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		if keys == nil {
			keys = []auth.APIKey{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok", "count": len(keys), "keys": keys,
		})

	case r.Method == http.MethodGet && path == "stats":
		stats := g.keyManager.Stats()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok", "stats": stats,
		})

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

		// Return the full key ONLY on creation — never shown again
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
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": `body must be {"key_or_id": "...", "revoked_by": "...", "reason": "..."}`})
			return
		}
		if err := g.keyManager.Revoke(body.KeyOrID, body.RevokedBy, body.Reason); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "revoked", "key_or_id": body.KeyOrID})

	case r.Method == http.MethodPost && path == "suspend":
		var body struct {
			ID int    `json:"id"`
			By string `json:"by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": `body must be {"id": <key_id>}`})
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
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": `body must be {"id": <key_id>}`})
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
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"reason": "valid routes: GET /admin/keys, GET /admin/keys/stats, POST /admin/keys/generate, POST /admin/keys/revoke, POST /admin/keys/suspend, POST /admin/keys/activate",
		})
	}
}

// HandleRetention, HandleIncidents, HandleReview, HandleRules, HandleAuditDetail
// are unchanged from the previous version — keeping them here for completeness

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
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": `body must be {"days": <number>}`})
			return
		}
		updated, err := g.retentionManager.UpdatePolicy(body.Days, body.UpdatedBy)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "updated", "policy": updated, "message": fmt.Sprintf("Logs older than %d days will be purged nightly", body.Days)})

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
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": `body must be {"api_key": "..."}`})
			return
		}
		result, err := g.retentionManager.EraseByAPIKey(body.APIKey)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "erased", "result": result, "message": fmt.Sprintf("GDPR erasure complete — %d audit log entries deleted", result.AuditLogsDeleted)})

	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "unknown retention route"})
	}
}

func (g *Gateway) HandleIncidents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/incidents"), "/")

	switch {
	case r.Method == http.MethodGet && path == "":
		incidents, err := g.incidentManager.GetAll(r.URL.Query().Get("severity"))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		if incidents == nil {
			incidents = []compliance.Incident{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "count": len(incidents), "incidents": incidents})

	case r.Method == http.MethodGet && path == "stats":
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "stats": g.incidentManager.Stats()})

	case r.Method == http.MethodPost && path == "resolve":
		var body struct {
			ID         int    `json:"id"`
			ResolvedBy string `json:"resolved_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": `body must be {"id": <id>}`})
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
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "resolved", "id": body.ID})

	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

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
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "stats": g.reviewQueue.Stats()})

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
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": `body must be {"id": <id>}`})
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
