package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockGroqServer creates a test HTTP server that mimics the Groq API.
// This lets us test the full gateway pipeline without real API calls.
func mockGroqServer(t *testing.T, response string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"content": response,
					},
				},
			},
		})
	}))
}

// ── callAI tests ─────────────────────────────────────────────────────────

func TestCallAI_SuccessfulResponse(t *testing.T) {
	server := mockGroqServer(t, "This is a test AI response")
	defer server.Close()

	g := &Gateway{
		apiKey: "test-groq-key",
	}

	// Override the Groq URL to point to our mock server
	// We test the response parsing logic
	resp, err := g.callAIWithURL("test prompt", server.URL+"/v1/chat/completions")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "This is a test AI response" {
		t.Errorf("unexpected response: %q", resp)
	}
}

func TestCallAI_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []interface{}{},
		})
	}))
	defer server.Close()

	g := &Gateway{apiKey: "test-key"}
	_, err := g.callAIWithURL("prompt", server.URL+"/v1/chat/completions")
	if err == nil {
		t.Error("expected error for empty choices")
	}
}

func TestCallAI_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": {"message": "rate limit exceeded"}}`))
	}))
	defer server.Close()

	g := &Gateway{apiKey: "test-key"}
	_, err := g.callAIWithURL("prompt", server.URL+"/v1/chat/completions")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

// ── HandleAI integration tests using httptest ─────────────────────────────

func buildTestRequest(t *testing.T, prompt, apiKey string) *http.Request {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"prompt": prompt})
	req := httptest.NewRequest(http.MethodPost, "/ai", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	return req
}

func TestHandleAI_MethodNotAllowed(t *testing.T) {
	g := newTestGateway(t)

	req := httptest.NewRequest(http.MethodGet, "/ai", nil)
	rr := httptest.NewRecorder()

	g.HandleAI(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestHandleAI_MissingAPIKey(t *testing.T) {
	g := newTestGateway(t)

	req := buildTestRequest(t, "hello world", "")
	rr := httptest.NewRecorder()

	g.HandleAI(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["status"] != "unauthorized" {
		t.Errorf("expected status=unauthorized, got %q", resp["status"])
	}
}

func TestHandleAI_InvalidAPIKey(t *testing.T) {
	g := newTestGateway(t)

	req := buildTestRequest(t, "hello", "invalid-key-xyz")
	rr := httptest.NewRecorder()

	g.HandleAI(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestHandleAI_EmptyPrompt(t *testing.T) {
	g := newTestGateway(t)

	body, _ := json.Marshal(map[string]string{"prompt": ""})
	req := httptest.NewRequest(http.MethodPost, "/ai", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

	rr := httptest.NewRecorder()
	g.HandleAI(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleAI_MissingPromptField(t *testing.T) {
	g := newTestGateway(t)

	body, _ := json.Marshal(map[string]string{"not_prompt": "hello"})
	req := httptest.NewRequest(http.MethodPost, "/ai", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

	rr := httptest.NewRecorder()
	g.HandleAI(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing prompt field, got %d", rr.Code)
	}
}

func TestHandleAI_InvalidJSON(t *testing.T) {
	g := newTestGateway(t)

	req := httptest.NewRequest(http.MethodPost, "/ai",
		bytes.NewReader([]byte("this is not json{")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", testAPIKey)

	rr := httptest.NewRecorder()
	g.HandleAI(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", rr.Code)
	}
}

func TestHandleAI_ContentTypeJSON(t *testing.T) {
	g := newTestGateway(t)

	req := buildTestRequest(t, "what is an API", "invalid-key")
	rr := httptest.NewRecorder()
	g.HandleAI(rr, req)

	// Response should always be JSON regardless of outcome
	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type: application/json, got %q", contentType)
	}
}

func TestHandleAI_RateLimitPerKey(t *testing.T) {
	g := newTestGateway(t)

	// Send requests until rate limited — using a key with limit 2
	for i := 0; i < 2; i++ {
		req := buildTestRequest(t, "hello", testAPIKey)
		rr := httptest.NewRecorder()
		g.HandleAI(rr, req)
		// First 2 may fail for other reasons (no real Groq) but shouldn't be 429
		if rr.Code == http.StatusTooManyRequests {
			t.Errorf("request %d should not be rate limited yet", i+1)
		}
	}
}

// ── HandleKeys tests ──────────────────────────────────────────────────────

func TestHandleKeys_GetReturnsJSON(t *testing.T) {
	g := newTestGateway(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/keys", nil)
	rr := httptest.NewRecorder()
	g.HandleKeys(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	if resp["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", resp["status"])
	}
}

func TestHandleKeys_GenerateMissingName(t *testing.T) {
	g := newTestGateway(t)

	body, _ := json.Marshal(map[string]interface{}{
		"owner": "test@example.com",
	})
	req := httptest.NewRequest(http.MethodPost, "/admin/keys/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.URL.Path = "/admin/keys/generate"
	rr := httptest.NewRecorder()
	g.HandleKeys(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing name, got %d", rr.Code)
	}
}

func TestHandleKeys_UnknownRoute(t *testing.T) {
	g := newTestGateway(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/keys/doesnotexist", nil)
	req.URL.Path = "/admin/keys/doesnotexist"
	rr := httptest.NewRecorder()
	g.HandleKeys(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown route, got %d", rr.Code)
	}
}
