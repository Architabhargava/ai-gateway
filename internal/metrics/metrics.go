package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// All metrics are registered in the default Prometheus registry.
// promauto.New* registers and returns in one call — no separate Register() needed.

var (
	// ── Request counters ──────────────────────────────────────────────────

	// RequestsTotal counts every request that hits the gateway, labelled by outcome
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_requests_total",
			Help: "Total number of requests processed by the gateway",
		},
		[]string{"status"}, // allowed, blocked, error, review_pending
	)

	// BlockedByCategory counts blocked requests broken down by what triggered the block
	BlockedByCategory = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_blocked_requests_total",
			Help: "Total number of blocked requests by block category",
		},
		[]string{"category"}, // jailbreak, prompt_injection, prohibited_content, etc.
	)

	// ProhibitedUseDetected counts Article 5 violations by EU AI Act article
	ProhibitedUseDetected = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_prohibited_use_total",
			Help: "Total EU AI Act Article 5 violations detected, by article",
		},
		[]string{"article"}, // Article 5(1)(a), Article 5(1)(c), etc.
	)

	// RateLimitHits counts how many requests were rejected by rate limiting
	RateLimitHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_rate_limit_hits_total",
			Help: "Total number of requests rejected by the rate limiter",
		},
		[]string{"api_key_name"}, // which key was rate limited
	)

	// ── Latency histograms ────────────────────────────────────────────────

	// RequestDuration measures end-to-end latency for every request
	// Buckets are in seconds — chosen to cover the expected range:
	// fast (< 100ms), normal (100ms-2s), slow (2s-10s), very slow (> 10s)
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gateway_request_duration_seconds",
			Help:    "End-to-end request duration in seconds",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0},
		},
		[]string{"status"}, // allowed, blocked, error
	)

	// ClassifierDuration measures how long the AI intent classifier takes
	ClassifierDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "gateway_classifier_duration_seconds",
			Help:    "AI intent classifier latency in seconds",
			Buckets: []float64{0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0},
		},
	)

	// GroqDuration measures how long the Groq API takes to respond
	GroqDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "gateway_groq_duration_seconds",
			Help:    "Groq API response latency in seconds",
			Buckets: []float64{0.1, 0.25, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0},
		},
	)

	// ReviewQueueWaitDuration measures how long requests wait in the human review queue
	ReviewQueueWaitDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "gateway_review_queue_wait_seconds",
			Help:    "How long requests waited in the human review queue",
			Buckets: []float64{1, 5, 10, 30, 60, 120, 300},
		},
	)

	// ── Gauges (current state) ────────────────────────────────────────────

	// ReviewQueuePending tracks how many requests are currently waiting for review
	ReviewQueuePending = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "gateway_review_queue_pending",
			Help: "Number of requests currently waiting for human review",
		},
	)

	// IncidentsUnresolved tracks how many security incidents are open
	IncidentsUnresolved = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "gateway_incidents_unresolved",
			Help: "Number of unresolved security incidents",
		},
	)

	// ActiveAPIKeys tracks how many API keys are currently active
	ActiveAPIKeys = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "gateway_active_api_keys",
			Help: "Number of active API keys",
		},
	)

	// ── Risk level counters ───────────────────────────────────────────────

	// RequestsByRiskLevel counts requests broken down by EU AI Act risk classification
	RequestsByRiskLevel = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gateway_requests_by_risk_level_total",
			Help: "Total requests by EU AI Act risk level",
		},
		[]string{"risk_level"}, // minimal, limited, high, unacceptable
	)
)
