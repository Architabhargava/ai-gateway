package auth

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
)

// AdminAuth provides HTTP Basic Auth middleware for the admin panel.
// Credentials are loaded from ADMIN_USERNAME and ADMIN_PASSWORD env vars.
// Uses constant-time comparison to prevent timing attacks.
type AdminAuth struct {
	username string
	password string
}

// NewAdminAuth creates an AdminAuth from environment variables
func NewAdminAuth() *AdminAuth {
	username := os.Getenv("ADMIN_USERNAME")
	password := os.Getenv("ADMIN_PASSWORD")

	if username == "" {
		username = "admin"
		fmt.Println("[AdminAuth] Warning: ADMIN_USERNAME not set — defaulting to 'admin'")
	}
	if password == "" {
		password = "changeme"
		fmt.Println("[AdminAuth] Warning: ADMIN_PASSWORD not set — defaulting to 'changeme'. Set a strong password in .env!")
	}

	fmt.Printf("[AdminAuth] Admin panel protected — username: %s\n", username)
	return &AdminAuth{username: username, password: password}
}

// Middleware wraps an http.HandlerFunc with Basic Auth protection.
// Returns 401 with WWW-Authenticate header if credentials are missing or wrong.
func (a *AdminAuth) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || !a.valid(user, pass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="AI Gateway Admin", charset="UTF-8"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Admin Login — AI Gateway</title>
<style>
  body{font-family:-apple-system,sans-serif;background:#0a0a12;color:#e0e0e0;display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
  .box{text-align:center;padding:40px;background:#111119;border:1px solid #2a2a3e;border-radius:14px;max-width:360px}
  h1{font-size:18px;font-weight:500;color:#fff;margin-bottom:8px}
  p{font-size:13px;color:#555;line-height:1.6;margin-bottom:20px}
  .badge{display:inline-block;background:#1a2a4e;color:#93c5fd;border-radius:4px;padding:2px 8px;font-size:11px;margin-bottom:16px}
</style>
</head>
<body>
<div class="box">
  <span class="badge">EU AI Act Compliant</span>
  <h1>AI Gateway Admin</h1>
  <p>This area is restricted to administrators only.<br>Enter your admin credentials to continue.</p>
  <p style="font-size:11px;color:#444">Your browser will show a login dialog.<br>Refresh the page if it does not appear.</p>
</div>
</body>
</html>`))
			return
		}
		next(w, r)
	}
}

// WrapHandler wraps an http.Handler (not just HandlerFunc) with Basic Auth
func (a *AdminAuth) WrapHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(a.Middleware(next.ServeHTTP))
}

// valid uses constant-time comparison to prevent timing attacks
func (a *AdminAuth) valid(user, pass string) bool {
	userMatch := subtle.ConstantTimeCompare([]byte(user), []byte(a.username)) == 1
	passMatch := subtle.ConstantTimeCompare([]byte(pass), []byte(a.password)) == 1
	return userMatch && passMatch
}
