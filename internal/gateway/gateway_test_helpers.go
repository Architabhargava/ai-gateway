package gateway

import (
	"ai-gateway/internal/auth"
	"ai-gateway/internal/compliance"
	"ai-gateway/internal/logger"
	"ai-gateway/internal/policy"
	"testing"
)

// testAPIKey is the key used in all tests
const testAPIKey = "gw_test_key_for_unit_tests_only"

// newTestGateway creates a fully wired gateway using in-memory storage.
// No database, no real Groq key, no real Resend key.
// Tests run fast and offline with no external dependencies.
func newTestGateway(t *testing.T) *Gateway {
	t.Helper()

	// In-memory logger — no SQLite file needed
	l, err := logger.New(":memory:")
	if err != nil {
		// Fall back to a temp logger
		l = &logger.Logger{}
	}

	// Key manager with the test key pre-seeded in cache
	km := auth.NewKeyManager(nil) // no DB

	// Manually seed our test key into the cache
	// This bypasses DB requirement for unit tests
	km.SeedTestKey(testAPIKey, "Test User", "test@example.com")

	return &Gateway{
		Name:             "Test Gateway",
		keyManager:       km,
		authLayer:        auth.NewAuth(km),
		policy:           policy.New(nil, ""), // no DB, no Groq key
		prohibited:       compliance.NewDetector(""),
		reviewQueue:      compliance.NewReviewQueue(nil),
		incidentManager:  compliance.NewIncidentManager(nil, "", "", ""),
		retentionManager: compliance.NewRetentionManager(nil),
		logger:           l,
		apiKey:           "", // no real Groq key in tests
	}
}
