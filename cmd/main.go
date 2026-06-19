package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"ai-gateway/internal/auth"
	"ai-gateway/internal/dashboard"
	"ai-gateway/internal/gateway"
	"ai-gateway/internal/logger"
)

var version = "dev"

func main() {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  AI Gateway %s — EU AI Act Compliant\n", version)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	dbPath := "gateway.db"
	if os.Getenv("RENDER") != "" {
		dbPath = "/tmp/gateway.db"
		fmt.Println("[Main] Running on Render — using /tmp/gateway.db")
	}

	l, err := logger.New(dbPath)
	if err != nil {
		log.Fatal("[Main] Logger failed:", err)
	}

	gw := gateway.New(l)
	dash := dashboard.New(l)
	adminAuth := auth.NewAdminAuth()

	mux := http.NewServeMux()

	// ── Public routes ─────────────────────────────────────────────────────
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			dash.HandleChat(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/chat", dash.HandleChat)
	mux.HandleFunc("/ai", gw.HandleAI)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	// ── Prometheus metrics endpoint ───────────────────────────────────────
	// Exposes all registered metrics in Prometheus text format.
	// Scraped by Grafana Cloud every 15 seconds.
	// No auth on purpose — metrics contain no sensitive data,
	// only aggregate counts and histograms.
	mux.Handle("/metrics", promhttp.Handler())

	// ── Admin routes (HTTP Basic Auth) ────────────────────────────────────
	mux.HandleFunc("/platform", adminAuth.Middleware(dash.HandlePlatform))
	mux.HandleFunc("/dashboard", adminAuth.Middleware(dash.HandleDashboard))
	mux.HandleFunc("/admin/audit/", adminAuth.Middleware(gw.HandleAuditDetail))
	mux.HandleFunc("/admin/rules", adminAuth.Middleware(gw.HandleRules))
	mux.HandleFunc("/admin/review", adminAuth.Middleware(gw.HandleReview))
	mux.HandleFunc("/admin/review/", adminAuth.Middleware(gw.HandleReview))
	mux.HandleFunc("/admin/incidents", adminAuth.Middleware(gw.HandleIncidents))
	mux.HandleFunc("/admin/incidents/", adminAuth.Middleware(gw.HandleIncidents))
	mux.HandleFunc("/admin/retention", adminAuth.Middleware(gw.HandleRetention))
	mux.HandleFunc("/admin/retention/", adminAuth.Middleware(gw.HandleRetention))
	mux.HandleFunc("/admin/keys", adminAuth.Middleware(gw.HandleKeys))
	mux.HandleFunc("/admin/keys/", adminAuth.Middleware(gw.HandleKeys))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Printf("  Listening on http://localhost:%s\n", port)
		fmt.Println("")
		fmt.Println("  PUBLIC:")
		fmt.Println("    /          Chat UI")
		fmt.Println("    /ai        Gateway endpoint")
		fmt.Println("    /metrics   Prometheus metrics")
		fmt.Println("    /health    Liveness probe")
		fmt.Println("")
		fmt.Println("  ADMIN (Basic Auth):")
		fmt.Println("    /platform  Admin platform")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Main] Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	fmt.Printf("\n[Main] Signal %s — shutting down gracefully...\n", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		fmt.Printf("[Main] Forced shutdown: %v\n", err)
	}

	l.Close()
	fmt.Println("[Main] Server stopped")
}
