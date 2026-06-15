package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"ai-gateway/internal/auth"
	"ai-gateway/internal/logger"
	"ai-gateway/internal/policy"
)

type Gateway struct {
	Name   string
	auth   *auth.Auth
	policy *policy.Engine
	logger *logger.Logger
	apiKey string
}

func New(l *logger.Logger) *Gateway {
	err := godotenv.Load()
	if err != nil {
		fmt.Println("[Warning] No .env file found")
	}

	apiKey := os.Getenv("GROQ_API_KEY")
	if apiKey == "" {
		fmt.Println("[Warning] GROQ_API_KEY not set")
	} else {
		fmt.Println("[Gateway] Groq API key loaded")
	}

	gatewayKeys := os.Getenv("GATEWAY_API_KEYS")
	if gatewayKeys == "" {
		fmt.Println("[Warning] GATEWAY_API_KEYS not set — all requests will be rejected")
	} else {
		count := len(strings.Split(gatewayKeys, ","))
		fmt.Printf("[Auth] Loaded %d API key(s)\n", count)
	}

	return &Gateway{
		Name:   "AI Gateway v1",
		auth:   auth.New(gatewayKeys),
		policy: policy.New(),
		logger: l,
		apiKey: apiKey,
	}
}

func (g *Gateway) HandleAI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST requests allowed", http.StatusMethodNotAllowed)
		return
	}

	valid, clientKey := g.auth.Validate(r)
	if !valid {
		fmt.Printf("[Auth] REJECTED request — invalid or missing API key\n")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "unauthorized",
			"reason": "Missing or invalid X-API-Key header",
		})
		return
	}

	fmt.Printf("[Auth] Accepted request with key: %s\n", clientKey)

	var body map[string]string
	err := json.NewDecoder(r.Body).Decode(&body)
	if err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	prompt, exists := body["prompt"]
	if !exists || prompt == "" {
		http.Error(w, "Missing 'prompt' field", http.StatusBadRequest)
		return
	}

	clientIP := r.RemoteAddr
	allowed, reason := g.policy.Check(clientIP, prompt)

	if !allowed {
		fmt.Printf("[Policy] BLOCKED request from %s — %s\n", clientIP, reason)
		if g.logger != nil {
			g.logger.Log(clientIP, prompt, "", "blocked", true, reason)
		}
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "blocked",
			"reason": reason,
		})
		return
	}

	fmt.Printf("[Gateway] Calling Groq for prompt: %s\n", prompt)
	response, err := g.callAI(prompt)
	if err != nil {
		fmt.Println("[Error] Groq call failed:", err)
		if g.logger != nil {
			g.logger.Log(clientIP, prompt, "", "error", false, err.Error())
		}
		http.Error(w, "AI service error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Printf("[Gateway] Groq responded successfully\n")
	if g.logger != nil {
		g.logger.Log(clientIP, prompt, response, "allowed", false, "")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "success",
		"prompt":   prompt,
		"response": response,
	})
}

func (g *Gateway) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Dashboard coming soon")
}
