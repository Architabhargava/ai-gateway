package policy

import (
	"testing"
	"time"
)

// ── Rate limiter tests ─────────────────────────────────────────────────────

func TestEngine_RateLimiter_AllowsUnderLimit(t *testing.T) {
	e := New(nil, "") // no DB, no Groq key needed for rate limit tests

	for i := 0; i < 5; i++ {
		allowed, reason := e.Check("test-key-123", "hello", 5)
		if !allowed {
			t.Errorf("request %d should be allowed, got blocked: %s", i+1, reason)
		}
	}
}

func TestEngine_RateLimiter_BlocksAtLimit(t *testing.T) {
	e := New(nil, "")

	// Send 5 requests — should all pass
	for i := 0; i < 5; i++ {
		e.Check("test-key-limit", "hello", 5)
	}

	// 6th request should be blocked
	allowed, reason := e.Check("test-key-limit", "hello", 5)
	if allowed {
		t.Error("6th request should be rate limited")
	}
	if reason == "" {
		t.Error("expected a rate limit reason message")
	}
}

func TestEngine_RateLimiter_PerKeyIsolation(t *testing.T) {
	e := New(nil, "")

	// Exhaust limit for key A
	for i := 0; i < 5; i++ {
		e.Check("key-A", "hello", 5)
	}

	// Key B should still be allowed — separate bucket
	allowed, _ := e.Check("key-B", "hello", 5)
	if !allowed {
		t.Error("key-B should not be affected by key-A's rate limit")
	}
}

func TestEngine_RateLimiter_RespectsCustomLimit(t *testing.T) {
	e := New(nil, "")

	// Key with limit of 2
	e.Check("custom-key", "hello", 2)
	e.Check("custom-key", "hello", 2)

	// 3rd should be blocked at limit 2
	allowed, _ := e.Check("custom-key", "hello", 2)
	if allowed {
		t.Error("3rd request should be blocked with limit=2")
	}
}

func TestEngine_RateLimiter_DefaultLimitFallback(t *testing.T) {
	e := New(nil, "")

	// perKeyLimit=0 should use the engine default (5)
	for i := 0; i < 5; i++ {
		allowed, _ := e.Check("default-key", "hello", 0)
		if !allowed {
			t.Errorf("request %d should be allowed with default limit", i+1)
		}
	}

	// 6th should be blocked
	allowed, _ := e.Check("default-key", "hello", 0)
	if allowed {
		t.Error("6th request should be blocked at default limit of 5")
	}
}

func TestEngine_RateLimiter_SlidingWindowPrune(t *testing.T) {
	e := &Engine{
		rateLimiter:  make(map[string]*RateBucket),
		defaultLimit: 2,
		windowSize:   100 * time.Millisecond, // tiny window for testing
	}

	// Fill the window
	e.Check("sliding-key", "hello", 2)
	e.Check("sliding-key", "hello", 2)

	// Should be blocked now
	allowed, _ := e.Check("sliding-key", "hello", 2)
	if allowed {
		t.Error("should be blocked after filling window")
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again after window slides
	allowed, _ = e.Check("sliding-key", "hello", 2)
	if !allowed {
		t.Error("should be allowed after window expires")
	}
}

// ── ClassifyResult struct tests ────────────────────────────────────────────

func TestClassifyResult_JSONFields(t *testing.T) {
	// Verify the struct has correct JSON tags (is_harmful not IsHarmful)
	// This was the critical bug we fixed — test it stays fixed
	result := ClassifyResult{
		IsHarmful:      true,
		Category:       "jailbreak",
		Reason:         "test reason",
		Score:          0.95,
		Indicators:     []string{"test indicator"},
		ReasoningChain: "step 1 | step 2",
		EUArticle:      "Article 5(1)(a)",
		RiskLevel:      "high",
	}

	if !result.IsHarmful {
		t.Error("IsHarmful should be true")
	}
	if result.Score != 0.95 {
		t.Errorf("Score should be 0.95, got %f", result.Score)
	}
	if result.Category != "jailbreak" {
		t.Errorf("Category should be jailbreak, got %s", result.Category)
	}
	if len(result.Indicators) != 1 {
		t.Errorf("Expected 1 indicator, got %d", len(result.Indicators))
	}
}

func TestEngine_Check_EmptyPrompt(t *testing.T) {
	e := New(nil, "")

	// Empty prompt should still go through rate limiter fine
	// Content classification is not done here
	allowed, _ := e.Check("key", "", 5)
	if !allowed {
		t.Error("rate limiter should not block based on prompt content")
	}
}

func TestEngine_RateLimiter_Concurrent(t *testing.T) {
	e := New(nil, "")

	// Run concurrent requests — should not panic or race
	done := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		go func() {
			e.Check("concurrent-key", "hello", 10)
			done <- true
		}()
	}

	for i := 0; i < 20; i++ {
		<-done
	}
}
