package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"ai-gateway/internal/auth"
	"ai-gateway/internal/compliance"
	"ai-gateway/internal/logger"
	"ai-gateway/internal/policy"
)

// Gateway is the core request handler
type Gateway struct {
	Name             string
	auth             *auth.Auth
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

	gatewayKeys := os.Getenv("GATEWAY_API_KEYS")
	if gatewayKeys == "" {
		fmt.Println("[Warning] GATEWAY_API_KEYS not set — all requests will be rejected")
	} else {
		fmt.Printf("[Auth] %d client API key(s) loaded\n", len(strings.Split(gatewayKeys, ",")))
	}

	resendKey := os.Getenv("RESEND_API_KEY")
	emailTo := os.Getenv("ALERT_EMAIL_TO")
	emailFrom := os.Getenv("ALERT_EMAIL_FROM")
	if emailFrom == "" {
		emailFrom = "onboarding@resend.dev"
	}

	return &Gateway{
		Name:             "AI Gateway v1",
		auth:             auth.New(gatewayKeys),
		policy:           policy.New(l.DB(), apiKey),
		prohibited:       compliance.NewDetector(apiKey),
		reviewQueue:      compliance.NewReviewQueue(l.DB()),
		incidentManager:  compliance.NewIncidentManager(l.DB(), resendKey, emailTo, emailFrom),
		retentionManager: compliance.NewRetentionManager(l.DB()),
		logger:           l,
		apiKey:           apiKey,
	}
}

// reviewDecision encapsulates why a request was routed to human review
type reviewDecision struct {
	needed bool
	reason string
}

// shouldReview determines if a request needs human review
func shouldReview(result policy.ClassifyResult) reviewDecision {
	sensitiveCategories := map[string]bool{
		"data_extraction":       true,
		"identity_manipulation": true,
		"prompt_injection":      true,
	}

	if sensitiveCategories[result.Category] {
		return reviewDecision{
			needed: true,
			reason: fmt.Sprintf(
				"sensitive category %q always requires human review (score: %.2f) — EU AI Act Article 14",
				result.Category, result.Score,
			),
		}
	}

	if result.IsHarmful && result.Score >= 0.4 && result.Score < 0.75 {
		return reviewDecision{
			needed: true,
			reason: fmt.Sprintf(
				"borderline classifier score %.2f — human review required per EU AI Act Article 14",
				result.Score,
			),
		}
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

	// ── Step 1: Authentication ────────────────────────────────────────────────
	valid, clientKey := g.auth.Validate(r)
	if !valid {
		fmt.Println("[Auth] Rejected — missing or invalid X-API-Key")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"status": "unauthorized", "reason": "missing or invalid X-API-Key header"})
		return
	}
	fmt.Printf("[Auth] Accepted — key: %s\n", clientKey)

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
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "prompt field is required and cannot be empty"})
		return
	}

	prompt := strings.TrimSpace(body.Prompt)
	clientIP := r.RemoteAddr

	// ── Step 3: Rate limit ────────────────────────────────────────────────────
	if allowed, reason := g.policy.Check(clientIP, prompt); !allowed {
		fmt.Printf("[Policy] Rate limited — IP: %s\n", clientIP)
		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "blocked",
			Blocked: true, Reason: reason, RiskLevel: logger.RiskLimited,
		})
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"status": "blocked", "reason": reason})
		return
	}

	// ── Step 4: Article 5 prohibited use detector ─────────────────────────────
	fmt.Println("[Prohibited] Checking Article 5 prohibited categories")
	prohibitedResult := g.prohibited.Check(prompt)

	if prohibitedResult.IsProhibited && prohibitedResult.Confidence >= 0.7 {
		reason := fmt.Sprintf("EU AI Act %s violation — %s: %s",
			prohibitedResult.Article, prohibitedResult.ArticleDescription, prohibitedResult.Reason)
		fmt.Printf("[Prohibited] BLOCKED (Article 5) — category: %s confidence: %.2f\n",
			prohibitedResult.Category, prohibitedResult.Confidence)

		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "blocked", Blocked: true,
			Reason: reason, ReasoningChain: "Article 5 prohibited use detector — " + prohibitedResult.Reason,
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

	// ── Step 5: AI intent classifier ─────────────────────────────────────────
	fmt.Println("[Policy] Running AI intent classifier")
	classifyResult := g.policy.ClassifyWithAI(prompt)
	riskLevel := mapRiskLevel(classifyResult.RiskLevel)
	refURL, refDesc := compliance.ArticleReference(classifyResult.EUArticle)

	// ── Step 5a: Review routing ───────────────────────────────────────────────
	review := shouldReview(classifyResult)

	if review.needed {
		fmt.Printf("[ReviewQueue] Routing to human review — %s\n", review.reason)

		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "review_pending", Blocked: false,
			Reason: review.reason, ReasoningChain: classifyResult.ReasoningChain,
			RiskLevel: riskLevel, EUArticle: classifyResult.EUArticle,
			Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
		})

		itemID, err := g.reviewQueue.Enqueue(0, prompt, clientIP,
			classifyResult.Category, classifyResult.ReasoningChain, classifyResult.Score)
		if err != nil {
			fmt.Println("[ReviewQueue] Enqueue failed — blocking as safe fallback:", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "review queue unavailable"})
			return
		}

		decision := g.reviewQueue.Poll(itemID)

		switch decision {
		case compliance.ReviewApproved:
			fmt.Printf("[ReviewQueue] Approved (id: %d) — forwarding to Groq\n", itemID)

		case compliance.ReviewRejected:
			rejReason := "rejected by human reviewer"
			severity := compliance.DetermineSeverity(classifyResult.RiskLevel, classifyResult.Category, classifyResult.EUArticle)
			go g.incidentManager.Create(0, severity, classifyResult.Category, classifyResult.EUArticle, prompt, clientIP, rejReason)
			g.writeLog(logger.LogEntry{
				ClientIP: clientIP, Prompt: prompt, Status: "blocked", Blocked: true,
				Reason: rejReason, ReasoningChain: classifyResult.ReasoningChain,
				RiskLevel: riskLevel, Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
			})
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "blocked", "reason": rejReason,
				"score": classifyResult.Score, "category": classifyResult.Category, "review_id": itemID,
			})
			return

		default:
			expReason := "human review timed out — blocked by default (EU AI Act Article 14)"
			g.writeLog(logger.LogEntry{
				ClientIP: clientIP, Prompt: prompt, Status: "blocked", Blocked: true,
				Reason: expReason, ReasoningChain: classifyResult.ReasoningChain,
				RiskLevel: riskLevel, Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
			})
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "blocked", "reason": expReason,
				"score": classifyResult.Score, "review_id": itemID,
			})
			return
		}
	}

	// ── Step 5b: High confidence auto-block ───────────────────────────────────
	if classifyResult.IsHarmful && classifyResult.Score >= 0.75 {
		reason := fmt.Sprintf("[%s] %s (confidence: %.0f%%)",
			classifyResult.Category, classifyResult.Reason, classifyResult.Score*100)

		fmt.Printf("[Policy] Auto-blocked — category: %s score: %.2f\n",
			classifyResult.Category, classifyResult.Score)

		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "blocked", Blocked: true,
			Reason: reason, ReasoningChain: classifyResult.ReasoningChain,
			RiskLevel: riskLevel, EUArticle: classifyResult.EUArticle,
			Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
		})

		severity := compliance.DetermineSeverity(classifyResult.RiskLevel, classifyResult.Category, classifyResult.EUArticle)
		go g.incidentManager.Create(0, severity, classifyResult.Category,
			classifyResult.EUArticle, prompt, clientIP, reason)

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
	fmt.Printf("[Gateway] Forwarding to Groq — prompt: %q\n", prompt)
	response, err := g.callAI(prompt)
	if err != nil {
		fmt.Printf("[Gateway] Groq error: %v\n", err)
		g.writeLog(logger.LogEntry{
			ClientIP: clientIP, Prompt: prompt, Status: "error", Blocked: false,
			Reason: err.Error(), ReasoningChain: classifyResult.ReasoningChain,
			RiskLevel: riskLevel, Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
		})
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "AI service error: " + err.Error()})
		return
	}

	fmt.Println("[Gateway] Groq responded successfully")
	g.writeLog(logger.LogEntry{
		ClientIP: clientIP, Prompt: prompt, Response: response, Status: "allowed", Blocked: false,
		ReasoningChain: classifyResult.ReasoningChain, RiskLevel: riskLevel,
		Category: classifyResult.Category, ClassifierScore: classifyResult.Score,
	})

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success", "prompt": prompt, "response": response, "risk_level": classifyResult.RiskLevel,
	})
}

// HandleRetention manages the log retention policy
//
//	GET  /admin/retention          — get current policy + storage stats
//	POST /admin/retention          — update retention days {days, updated_by}
//	POST /admin/retention/purge    — trigger manual purge immediately
//	DELETE /admin/retention/erase  — GDPR erasure for an API key {api_key}
func (g *Gateway) HandleRetention(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/admin/retention")
	path = strings.Trim(path, "/")

	switch {

	case r.Method == http.MethodGet && path == "":
		// Return current policy + storage stats
		policy, err := g.retentionManager.GetPolicy()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		stats := g.retentionManager.StorageStats()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"policy":  policy,
			"storage": stats,
		})

	case r.Method == http.MethodPost && path == "":
		// Update retention policy
		var body struct {
			Days      int    `json:"days"`
			UpdatedBy string `json:"updated_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Days == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "error",
				"reason": `body must be {"days": <number>, "updated_by": "<name>"}`,
			})
			return
		}
		updated, err := g.retentionManager.UpdatePolicy(body.Days, body.UpdatedBy)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "updated",
			"policy":  updated,
			"message": fmt.Sprintf("Logs older than %d days will be purged nightly", body.Days),
		})

	case r.Method == http.MethodPost && path == "purge":
		// Manual purge trigger
		result, err := g.retentionManager.Purge()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "purged",
			"result": result,
		})

	case r.Method == http.MethodDelete && path == "erase":
		// GDPR right to erasure — delete all logs for an API key
		var body struct {
			APIKey string `json:"api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.APIKey == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "error",
				"reason": `body must be {"api_key": "<key>"}`,
			})
			return
		}
		result, err := g.retentionManager.EraseByAPIKey(body.APIKey)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "erased",
			"result":  result,
			"message": fmt.Sprintf("GDPR erasure complete — %d audit log entries deleted for key %s", result.AuditLogsDeleted, result.APIKey),
		})

	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"reason": "valid routes: GET /admin/retention, POST /admin/retention, POST /admin/retention/purge, DELETE /admin/retention/erase",
		})
	}
}

// HandleIncidents serves the incident management API
func (g *Gateway) HandleIncidents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/admin/incidents")
	path = strings.Trim(path, "/")

	switch {
	case r.Method == http.MethodGet && path == "":
		severity := r.URL.Query().Get("severity")
		incidents, err := g.incidentManager.GetAll(severity)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		if incidents == nil {
			incidents = []compliance.Incident{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok", "count": len(incidents), "incidents": incidents,
		})

	case r.Method == http.MethodGet && path == "stats":
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok", "stats": g.incidentManager.Stats(),
		})

	case r.Method == http.MethodPost && path == "resolve":
		var body struct {
			ID         int    `json:"id"`
			ResolvedBy string `json:"resolved_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "error", "reason": `body must be {"id": <incident_id>, "resolved_by": "<name>"}`,
			})
			return
		}
		resolvedBy := body.ResolvedBy
		if resolvedBy == "" {
			resolvedBy = "admin"
		}
		if err := g.incidentManager.Resolve(body.ID, resolvedBy); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "resolved", "id": body.ID, "resolved_by": resolvedBy,
		})

	default:
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"reason": "valid routes: GET /admin/incidents, GET /admin/incidents/stats, POST /admin/incidents/resolve",
		})
	}
}

// HandleReview serves the human review queue admin endpoints
func (g *Gateway) HandleReview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/admin/review")
	path = strings.Trim(path, "/")

	switch {
	case r.Method == http.MethodGet && path == "":
		items, err := g.reviewQueue.GetPending()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
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
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
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
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "unknown review route"})
	}
}

func (g *Gateway) handleDecision(w http.ResponseWriter, r *http.Request, status compliance.ReviewStatus) {
	var body struct {
		ID       int    `json:"id"`
		Reviewer string `json:"reviewer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": `body must be {"id": <id>, "reviewer": "<name>"}`})
		return
	}
	reviewer := body.Reviewer
	if reviewer == "" {
		reviewer = "admin"
	}
	if err := g.reviewQueue.Decide(body.ID, status, reviewer); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok", "decision": string(status), "id": body.ID, "reviewer": reviewer,
	})
}

// HandleRules manages blocked keyword rules
func (g *Gateway) HandleRules(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		rules, err := g.policy.GetRules()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "count": len(rules), "rules": rules})

	case http.MethodPost:
		var body struct {
			Word string `json:"word"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Word == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "word field required"})
			return
		}
		if err := g.policy.AddRule(body.Word); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "added", "word": body.Word})

	case http.MethodDelete:
		var body struct {
			Word string `json:"word"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Word == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "word field required"})
			return
		}
		if err := g.policy.RemoveRule(body.Word); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "removed", "word": body.Word})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "method not allowed"})
	}
}

// HandleAuditDetail returns the full audit record for a single request ID
func (g *Gateway) HandleAuditDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "missing audit log ID"})
		return
	}

	var id int
	fmt.Sscanf(parts[2], "%d", &id)
	if id == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": "invalid audit log ID"})
		return
	}

	entry, err := g.logger.GetByID(id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "reason": err.Error()})
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
		fmt.Printf("[Gateway] Failed to write log: %v\n", err)
	}
}

func (g *Gateway) writeLogWithReturn(entry logger.LogEntry) error {
	if g.logger == nil {
		return fmt.Errorf("logger not available")
	}
	return g.logger.Log(entry)
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
