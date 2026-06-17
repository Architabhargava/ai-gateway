package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

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

	l, err := logger.New(dbPath)
	if err != nil {
		log.Fatal("[Main] Logger failed to initialise:", err)
	}
	defer l.Close()

	gw := gateway.New(l)
	dash := dashboard.New(l)

	mux := http.NewServeMux()

	// ── Unified platform (single URL, all features) ───────────────────────────
	mux.HandleFunc("/platform", dash.HandlePlatform)

	// ── Root redirects to platform ────────────────────────────────────────────
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/platform", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// ── Gateway endpoint ──────────────────────────────────────────────────────
	mux.HandleFunc("/ai", gw.HandleAI)

	// ── Dashboard data API (JSON for platform) ────────────────────────────────
	mux.HandleFunc("/dashboard", dash.HandleDashboard)

	// ── Liveness probe ────────────────────────────────────────────────────────
	mux.HandleFunc("/health", handleHealth)

	// ── Admin API — audit detail ──────────────────────────────────────────────
	mux.HandleFunc("/admin/audit/", gw.HandleAuditDetail)

	// ── Admin API — keyword rules ─────────────────────────────────────────────
	mux.HandleFunc("/admin/rules", gw.HandleRules)

	// ── Admin API — human review queue ────────────────────────────────────────
	mux.HandleFunc("/admin/review", gw.HandleReview)
	mux.HandleFunc("/admin/review/", gw.HandleReview)

	// ── Admin API — incidents ─────────────────────────────────────────────────
	mux.HandleFunc("/admin/incidents", gw.HandleIncidents)
	mux.HandleFunc("/admin/incidents/", gw.HandleIncidents)

	// ── Admin API — retention + GDPR erasure ──────────────────────────────────
	mux.HandleFunc("/admin/retention", gw.HandleRetention)
	mux.HandleFunc("/admin/retention/", gw.HandleRetention)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  Open http://localhost:%s/platform\n", port)
	fmt.Println("  Everything is in one place.")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal("[Main] Server failed to start:", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}
