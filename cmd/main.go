package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"ai-gateway/internal/auth"
	"ai-gateway/internal/dashboard"
	"ai-gateway/internal/gateway"
	"ai-gateway/internal/logger"
)

func main() {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  AI Gateway — EU AI Act Compliant")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	dbPath := "gateway.db"
	if os.Getenv("RENDER") != "" {
		dbPath = "/tmp/gateway.db"
		fmt.Println("[Main] Running on Render — using /tmp/gateway.db")
	}

	// ── Initialise shared dependencies ────────────────────────────────────────
	l, err := logger.New(dbPath)
	if err != nil {
		log.Fatal("[Main] Logger failed:", err)
	}
	defer l.Close()

	gw := gateway.New(l)
	dash := dashboard.New(l)
	admin := auth.NewAdminAuth()

	mux := http.NewServeMux()

	// ── PUBLIC routes — anyone can access ─────────────────────────────────────
	// Root → user chat interface (no login required, uses API key auth)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			dash.HandleChat(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/chat", dash.HandleChat)

	// Gateway API endpoint — authenticated by X-API-Key header
	mux.HandleFunc("/ai", gw.HandleAI)

	// Liveness probe — for deployment platforms
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	// ── ADMIN routes — protected by HTTP Basic Auth ───────────────────────────
	// All /platform and /admin/* routes require admin credentials
	// Users are completely locked out of these endpoints

	// Admin platform UI
	mux.HandleFunc("/platform", admin.Middleware(dash.HandlePlatform))

	// Dashboard data API (JSON) — used by the platform UI
	mux.HandleFunc("/dashboard", admin.Middleware(dash.HandleDashboard))

	// Admin API — audit detail
	mux.HandleFunc("/admin/audit/", admin.Middleware(gw.HandleAuditDetail))

	// Admin API — keyword rules
	mux.HandleFunc("/admin/rules", admin.Middleware(gw.HandleRules))

	// Admin API — human review queue
	mux.HandleFunc("/admin/review", admin.Middleware(gw.HandleReview))
	mux.HandleFunc("/admin/review/", admin.Middleware(gw.HandleReview))

	// Admin API — incidents
	mux.HandleFunc("/admin/incidents", admin.Middleware(gw.HandleIncidents))
	mux.HandleFunc("/admin/incidents/", admin.Middleware(gw.HandleIncidents))

	// Admin API — retention + GDPR erasure
	mux.HandleFunc("/admin/retention", admin.Middleware(gw.HandleRetention))
	mux.HandleFunc("/admin/retention/", admin.Middleware(gw.HandleRetention))

	// Admin API — API key management
	mux.HandleFunc("/admin/keys", admin.Middleware(gw.HandleKeys))
	mux.HandleFunc("/admin/keys/", admin.Middleware(gw.HandleKeys))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  Listening on http://localhost:%s\n", port)
	fmt.Println("")
	fmt.Println("  PUBLIC (users):")
	fmt.Println("    http://localhost:" + port + "/          Chat UI")
	fmt.Println("    http://localhost:" + port + "/ai        Gateway endpoint")
	fmt.Println("")
	fmt.Println("  ADMIN (password protected):")
	fmt.Println("    http://localhost:" + port + "/platform  Full admin platform")
	fmt.Println("")
	fmt.Println("  Admin credentials from .env:")
	fmt.Println("    ADMIN_USERNAME / ADMIN_PASSWORD")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
