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

	// ── User-facing pages ─────────────────────────────────────────────────────
	mux.HandleFunc("/", dash.HandleHome)
	mux.HandleFunc("/dashboard", dash.HandleDashboard)
	mux.HandleFunc("/review", dash.HandleReviewPage)
	mux.HandleFunc("/incidents", dash.HandleIncidentsPage)
	mux.HandleFunc("/retention", dash.HandleRetentionPage)
	mux.HandleFunc("/health", handleHealth)

	// ── Core gateway ──────────────────────────────────────────────────────────
	mux.HandleFunc("/ai", gw.HandleAI)

	// ── Admin API — audit ─────────────────────────────────────────────────────
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
	fmt.Printf("  Listening on http://localhost:%s\n", port)
	fmt.Println("")
	fmt.Println("  Pages:")
	fmt.Println("    /              Chat UI")
	fmt.Println("    /dashboard     Audit log")
	fmt.Println("    /review        Human review queue")
	fmt.Println("    /incidents     Incident dashboard")
	fmt.Println("    /retention     Retention & GDPR erasure")
	fmt.Println("    /health        Liveness probe")
	fmt.Println("")
	fmt.Println("  Admin API:")
	fmt.Println("    POST   /ai                          Gateway endpoint")
	fmt.Println("    GET    /admin/audit/:id             Full reasoning chain")
	fmt.Println("    *      /admin/rules                 Keyword rules")
	fmt.Println("    GET    /admin/review                Pending queue")
	fmt.Println("    POST   /admin/review/approve        Approve item")
	fmt.Println("    POST   /admin/review/reject         Reject item")
	fmt.Println("    GET    /admin/incidents             All incidents")
	fmt.Println("    POST   /admin/incidents/resolve     Resolve incident")
	fmt.Println("    GET    /admin/retention             Policy + storage stats")
	fmt.Println("    POST   /admin/retention             Update retention days")
	fmt.Println("    POST   /admin/retention/purge       Manual purge")
	fmt.Println("    DELETE /admin/retention/erase       GDPR right to erasure")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal("[Main] Server failed to start:", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}
