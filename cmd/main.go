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
	}

	l, err := logger.New(dbPath)
	if err != nil {
		log.Fatal("[Main] Logger failed:", err)
	}
	defer l.Close()

	gw := gateway.New(l)
	dash := dashboard.New(l)

	mux := http.NewServeMux()

	// Platform UI
	mux.HandleFunc("/platform", dash.HandlePlatform)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/platform", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	})

	// Gateway endpoint
	mux.HandleFunc("/ai", gw.HandleAI)

	// Dashboard data (JSON)
	mux.HandleFunc("/dashboard", dash.HandleDashboard)

	// Liveness
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	// Admin API
	mux.HandleFunc("/admin/audit/", gw.HandleAuditDetail)
	mux.HandleFunc("/admin/rules", gw.HandleRules)
	mux.HandleFunc("/admin/review", gw.HandleReview)
	mux.HandleFunc("/admin/review/", gw.HandleReview)
	mux.HandleFunc("/admin/incidents", gw.HandleIncidents)
	mux.HandleFunc("/admin/incidents/", gw.HandleIncidents)
	mux.HandleFunc("/admin/retention", gw.HandleRetention)
	mux.HandleFunc("/admin/retention/", gw.HandleRetention)
	mux.HandleFunc("/admin/keys", gw.HandleKeys)
	mux.HandleFunc("/admin/keys/", gw.HandleKeys)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("  Open http://localhost:%s/platform\n", port)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
