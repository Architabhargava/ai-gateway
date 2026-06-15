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
	fmt.Println("Starting AI Gateway...")
	fmt.Println("Listening on http://localhost:8080")

	dbPath := "gateway.db"
	if os.Getenv("RENDER") != "" {
		dbPath = "/tmp/gateway.db"
	}

	l, err := logger.New(dbPath)
	if err != nil {
		log.Fatal("Logger failed to start:", err)
	}
	defer l.Close()

	gw := gateway.New(l)
	dash := dashboard.New(l)

	mux := http.NewServeMux()
	mux.HandleFunc("/", dash.HandleHome)
	mux.HandleFunc("/ai", gw.HandleAI)
	mux.HandleFunc("/dashboard", dash.HandleDashboard)
	mux.HandleFunc("/health", handleHealth)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Println("Listening on port", port)
	err = http.ListenAndServe(":"+port, mux)
	if err != nil {
		log.Fatal("Server failed to start:", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Gateway is running")
}
