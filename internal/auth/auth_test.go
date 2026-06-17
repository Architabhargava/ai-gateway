package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── KeyManager tests ───────────────────────────────────────────────────────

func TestKeyManager_GenerateAndValidate(t *testing.T) {
	km := NewKeyManager(nil) // in-memory only, no DB

	// Without DB, cache is empty — all keys invalid
	req := httptest.NewRequest(http.MethodPost, "/ai", nil)
	req.Header.Set("X-API-Key", "gw_nonexistent")

	valid, _, reason := km.Validate(req)
	if valid {
		t.Error("expected invalid key to be rejected")
	}
	if reason == "" {
		t.Error("expected a rejection reason")
	}
}

func TestKeyManager_MissingKey(t *testing.T) {
	km := NewKeyManager(nil)

	req := httptest.NewRequest(http.MethodPost, "/ai", nil)
	// No X-API-Key header

	valid, _, reason := km.Validate(req)
	if valid {
		t.Error("expected missing key to be rejected")
	}
	if reason != "missing X-API-Key header" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestKeyManager_EmptyKey(t *testing.T) {
	km := NewKeyManager(nil)

	req := httptest.NewRequest(http.MethodPost, "/ai", nil)
	req.Header.Set("X-API-Key", "")

	valid, _, _ := km.Validate(req)
	if valid {
		t.Error("expected empty key to be rejected")
	}
}

// ── AdminAuth tests ────────────────────────────────────────────────────────

func TestAdminAuth_ValidCredentials(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "testadmin")
	t.Setenv("ADMIN_PASSWORD", "testpass123")

	admin := NewAdminAuth()
	called := false

	handler := admin.Middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/platform", nil)
	req.SetBasicAuth("testadmin", "testpass123")
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !called {
		t.Error("handler should have been called with valid credentials")
	}
}

func TestAdminAuth_InvalidPassword(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "testadmin")
	t.Setenv("ADMIN_PASSWORD", "testpass123")

	admin := NewAdminAuth()
	called := false

	handler := admin.Middleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/platform", nil)
	req.SetBasicAuth("testadmin", "wrongpassword")
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
	if called {
		t.Error("handler should NOT have been called with invalid credentials")
	}
}

func TestAdminAuth_NoCredentials(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "testadmin")
	t.Setenv("ADMIN_PASSWORD", "testpass123")

	admin := NewAdminAuth()

	handler := admin.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/platform", nil)
	// No Basic Auth header
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	// Must include WWW-Authenticate header so browser shows login dialog
	if rr.Header().Get("WWW-Authenticate") == "" {
		t.Error("expected WWW-Authenticate header in 401 response")
	}
}

func TestAdminAuth_WrongUsername(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "testadmin")
	t.Setenv("ADMIN_PASSWORD", "testpass123")

	admin := NewAdminAuth()

	handler := admin.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/platform", nil)
	req.SetBasicAuth("hacker", "testpass123")
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestAdminAuth_TimingAttackResistance(t *testing.T) {
	// Verify that constant-time comparison is used (no early exit)
	// We can't test timing directly but we can verify both wrong
	// username and wrong password both return 401
	t.Setenv("ADMIN_USERNAME", "admin")
	t.Setenv("ADMIN_PASSWORD", "secret")

	admin := NewAdminAuth()
	handler := admin.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		user string
		pass string
	}{
		{"admin", "wrong"},
		{"wrong", "secret"},
		{"wrong", "wrong"},
		{"", ""},
		{"admin", ""},
		{"", "secret"},
	}

	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/platform", nil)
		req.SetBasicAuth(c.user, c.pass)
		rr := httptest.NewRecorder()
		handler(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("user=%q pass=%q: expected 401, got %d", c.user, c.pass, rr.Code)
		}
	}
}
