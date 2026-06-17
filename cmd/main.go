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

	"ai-gateway/internal/auth"
	"ai-gateway/internal/dashboard"
	"ai-gateway/internal/gateway"
	"ai-gateway/internal/logger"
)

// version is injected at build time via -ldflags
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

	// ── Initialise dependencies ───────────────────────────────────────────────
	l, err := logger.New(dbPath)
	if err != nil {
		log.Fatal("[Main] Logger failed:", err)
	}

	gw := gateway.New(l)
	dash := dashboard.New(l)
	adminAuth := auth.NewAdminAuth()

	// ── Register routes ───────────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Public routes — users
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			dash.HandleChat(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/chat", dash.HandleChat)
	mux.HandleFunc("/ai", gw.HandleAI)
	mux.HandleFunc("/health", handleHealth)

	// Admin routes — protected by HTTP Basic Auth
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

	// ── Start server with graceful shutdown ───────────────────────────────────
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second, // long timeout for review queue polling
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background goroutine
	go func() {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Printf("  Listening on http://localhost:%s\n", port)
		fmt.Println("")
		fmt.Println("  PUBLIC (users):")
		fmt.Println("    http://localhost:" + port + "/          Chat UI")
		fmt.Println("    http://localhost:" + port + "/ai        Gateway endpoint")
		fmt.Println("")
		fmt.Println("  ADMIN (password protected):")
		fmt.Println("    http://localhost:" + port + "/platform  Admin platform")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[Main] Server error: %v", err)
		}
	}()

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	// Wait for OS signal (SIGTERM from Render/Kubernetes, SIGINT from Ctrl+C)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	fmt.Printf("\n[Main] Received signal: %s — shutting down gracefully...\n", sig)

	// Give in-flight requests up to 30 seconds to complete
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		fmt.Printf("[Main] Forced shutdown after timeout: %v\n", err)
	}

	// Close database connection cleanly
	l.Close()
	fmt.Println("[Main] Server stopped — goodbye")
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}
