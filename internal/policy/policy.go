package policy

import (
	"strings"
	"sync"
	"time"
)

type Engine struct {
	blockedWords []string
	rateLimiter  map[string][]time.Time
	maxRequests  int
	windowSize   time.Duration
	mu           sync.Mutex
}

func New() *Engine {
	return &Engine{
		blockedWords: []string{
			"jailbreak",
			"ignore instructions",
			"ignore previous",
			"bypass",
			"pretend you are",
		},
		rateLimiter: make(map[string][]time.Time),
		maxRequests: 5,
		windowSize:  time.Minute,
	}
}

func (e *Engine) Check(clientIP, prompt string) (allowed bool, reason string) {
	if blocked, word := e.containsBlockedWord(prompt); blocked {
		return false, "Prompt contains blocked word: " + word
	}

	if limited, remaining := e.isRateLimited(clientIP); limited {
		return false, "Rate limit exceeded. Try again in " + remaining.String()
	}

	return true, ""
}

func (e *Engine) containsBlockedWord(prompt string) (bool, string) {
	lower := strings.ToLower(prompt)
	for _, word := range e.blockedWords {
		if strings.Contains(lower, word) {
			return true, word
		}
	}
	return false, ""
}

func (e *Engine) isRateLimited(clientIP string) (bool, time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-e.windowSize)

	requests := e.rateLimiter[clientIP]
	var recent []time.Time
	for _, t := range requests {
		if t.After(windowStart) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= e.maxRequests {
		oldest := recent[0]
		waitTime := oldest.Add(e.windowSize).Sub(now)
		return true, waitTime
	}

	e.rateLimiter[clientIP] = append(recent, now)
	return false, 0
}
