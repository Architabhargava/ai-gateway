package auth

import "time"

// SeedTestKey adds a key directly to the in-memory cache.
// Only used in tests — allows unit tests to run without a database.
func (km *KeyManager) SeedTestKey(key, name, owner string) {
	km.mu.Lock()
	defer km.mu.Unlock()
	km.cache[key] = &APIKey{
		ID:        999,
		Key:       key,
		Name:      name,
		Owner:     owner,
		Status:    KeyActive,
		RateLimit: 0,
		CreatedAt: time.Now(),
	}
}
