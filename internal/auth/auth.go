package auth

import (
	"net/http"
)

// Auth wraps the KeyManager for request validation
// Kept as a thin adapter so the rest of the codebase doesn't change
type Auth struct {
	km *KeyManager
}

// NewAuth creates an Auth backed by the KeyManager
func NewAuth(km *KeyManager) *Auth {
	return &Auth{km: km}
}

// Validate checks the X-API-Key header against the key manager
func (a *Auth) Validate(r *http.Request) (valid bool, rawKey string, reason string) {
	ok, key, reason := a.km.Validate(r)
	if !ok {
		return false, r.Header.Get("X-API-Key"), reason
	}
	return true, key.Key, ""
}

// ValidateFull returns the full APIKey record for downstream use (rate limits etc)
func (a *Auth) ValidateFull(r *http.Request) (valid bool, key *APIKey, reason string) {
	return a.km.Validate(r)
}
