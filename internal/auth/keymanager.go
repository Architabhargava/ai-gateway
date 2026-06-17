package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// KeyStatus represents the lifecycle state of an API key
type KeyStatus string

const (
	KeyActive    KeyStatus = "active"
	KeyRevoked   KeyStatus = "revoked"
	KeySuspended KeyStatus = "suspended" // temporarily disabled, can be re-activated
	KeyExpired   KeyStatus = "expired"
)

// APIKey represents a managed gateway API key with full metadata
type APIKey struct {
	ID           int        `json:"id"`
	Key          string     `json:"key"`
	Name         string     `json:"name"`  // human label: "Team Alpha", "Customer X"
	Owner        string     `json:"owner"` // email or team name
	Status       KeyStatus  `json:"status"`
	RateLimit    int        `json:"rate_limit"` // requests per minute, 0 = use default
	CreatedAt    time.Time  `json:"created_at"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	RevokedBy    string     `json:"revoked_by,omitempty"`
	RevokeReason string     `json:"revoke_reason,omitempty"`
	RequestCount int        `json:"request_count"`
	BlockCount   int        `json:"block_count"`
}

// KeyManager manages API keys in the database
type KeyManager struct {
	db    *sql.DB
	cache map[string]*APIKey // in-memory cache for fast lookup
	mu    sync.RWMutex
}

// NewKeyManager creates and initialises the key manager
func NewKeyManager(db *sql.DB) *KeyManager {
	km := &KeyManager{
		db:    db,
		cache: make(map[string]*APIKey),
	}

	if db != nil {
		km.initDB()
		km.loadCache()
	}

	fmt.Println("[KeyManager] Initialised")
	return km
}

// initDB creates the api_keys table
func (km *KeyManager) initDB() {
	_, err := km.db.Exec(`
		CREATE TABLE IF NOT EXISTS api_keys (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			key           TEXT    UNIQUE NOT NULL,
			name          TEXT    NOT NULL DEFAULT '',
			owner         TEXT    NOT NULL DEFAULT '',
			status        TEXT    NOT NULL DEFAULT 'active',
			rate_limit    INTEGER NOT NULL DEFAULT 0,
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used_at  DATETIME,
			expires_at    DATETIME,
			revoked_at    DATETIME,
			revoked_by    TEXT    NOT NULL DEFAULT '',
			revoke_reason TEXT    NOT NULL DEFAULT '',
			request_count INTEGER NOT NULL DEFAULT 0,
			block_count   INTEGER NOT NULL DEFAULT 0
		)`)
	if err != nil {
		fmt.Println("[KeyManager] Failed to create api_keys table:", err)
		return
	}

	count := 0
	km.db.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE status = 'active'`).Scan(&count)
	fmt.Printf("[KeyManager] Table ready — %d active keys\n", count)
}

// loadCache loads all active keys into memory for O(1) validation
func (km *KeyManager) loadCache() {
	rows, err := km.db.Query(`
		SELECT id, key, name, owner, status, rate_limit,
		       created_at, last_used_at, expires_at,
		       revoked_at, revoked_by, revoke_reason,
		       request_count, block_count
		FROM api_keys`)
	if err != nil {
		fmt.Println("[KeyManager] Failed to load cache:", err)
		return
	}
	defer rows.Close()

	km.mu.Lock()
	defer km.mu.Unlock()
	km.cache = make(map[string]*APIKey)

	for rows.Next() {
		k := &APIKey{}
		var lastUsed, expires, revokedAt sql.NullString
		var createdStr string

		if err := rows.Scan(
			&k.ID, &k.Key, &k.Name, &k.Owner, &k.Status, &k.RateLimit,
			&createdStr, &lastUsed, &expires,
			&revokedAt, &k.RevokedBy, &k.RevokeReason,
			&k.RequestCount, &k.BlockCount,
		); err != nil {
			continue
		}

		k.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		if lastUsed.Valid && lastUsed.String != "" {
			t, _ := time.Parse("2006-01-02 15:04:05", lastUsed.String)
			k.LastUsedAt = &t
		}
		if expires.Valid && expires.String != "" {
			t, _ := time.Parse("2006-01-02 15:04:05", expires.String)
			k.ExpiresAt = &t
		}
		if revokedAt.Valid && revokedAt.String != "" {
			t, _ := time.Parse("2006-01-02 15:04:05", revokedAt.String)
			k.RevokedAt = &t
		}

		km.cache[k.Key] = k
	}

	fmt.Printf("[KeyManager] Cache loaded — %d keys\n", len(km.cache))
}

// Validate checks if a key is valid and active
// Returns the key record and a rejection reason if invalid
func (km *KeyManager) Validate(r *http.Request) (valid bool, key *APIKey, reason string) {
	rawKey := r.Header.Get("X-API-Key")
	if rawKey == "" {
		return false, nil, "missing X-API-Key header"
	}

	km.mu.RLock()
	k, exists := km.cache[rawKey]
	km.mu.RUnlock()

	if !exists {
		return false, nil, "invalid API key"
	}

	// Check status
	switch k.Status {
	case KeyRevoked:
		reason := "API key has been revoked"
		if k.RevokeReason != "" {
			reason += ": " + k.RevokeReason
		}
		return false, k, reason
	case KeySuspended:
		return false, k, "API key is temporarily suspended"
	case KeyExpired:
		return false, k, "API key has expired"
	}

	// Check expiry
	if k.ExpiresAt != nil && time.Now().After(*k.ExpiresAt) {
		// Auto-mark as expired
		go km.markExpired(k.Key)
		return false, k, "API key has expired"
	}

	// Valid — update last used and request count in background
	go km.recordUsage(k.Key, false)

	return true, k, ""
}

// Generate creates a new API key with a cryptographically secure random value
func (km *KeyManager) Generate(name, owner string, rateLimit int, expiresAt *time.Time) (*APIKey, error) {
	if km.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	// Generate a 32-byte random key with a recognisable prefix
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	key := "gw_" + hex.EncodeToString(raw)

	var expiresStr interface{} = nil
	if expiresAt != nil {
		expiresStr = expiresAt.Format("2006-01-02 15:04:05")
	}

	result, err := km.db.Exec(`
		INSERT INTO api_keys (key, name, owner, status, rate_limit, expires_at)
		VALUES (?, ?, ?, 'active', ?, ?)`,
		key, name, owner, rateLimit, expiresStr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert key: %w", err)
	}

	id, _ := result.LastInsertId()
	k := &APIKey{
		ID:        int(id),
		Key:       key,
		Name:      name,
		Owner:     owner,
		Status:    KeyActive,
		RateLimit: rateLimit,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}

	// Add to cache
	km.mu.Lock()
	km.cache[key] = k
	km.mu.Unlock()

	fmt.Printf("[KeyManager] Generated key for %s (%s) — id: %d\n", name, owner, id)
	return k, nil
}

// Revoke permanently disables a key with a reason
func (km *KeyManager) Revoke(keyOrID string, revokedBy, reason string) error {
	if km.db == nil {
		return fmt.Errorf("database not available")
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	// Support revoking by key value or by ID
	var err error
	if strings.HasPrefix(keyOrID, "gw_") {
		_, err = km.db.Exec(`
			UPDATE api_keys SET status='revoked', revoked_at=?, revoked_by=?, revoke_reason=?
			WHERE key=? AND status='active'`,
			now, revokedBy, reason, keyOrID)
	} else {
		_, err = km.db.Exec(`
			UPDATE api_keys SET status='revoked', revoked_at=?, revoked_by=?, revoke_reason=?
			WHERE id=? AND status!='revoked'`,
			now, revokedBy, reason, keyOrID)
	}

	if err != nil {
		return fmt.Errorf("failed to revoke key: %w", err)
	}

	// Update cache
	km.mu.Lock()
	for _, k := range km.cache {
		if k.Key == keyOrID || fmt.Sprintf("%d", k.ID) == keyOrID {
			k.Status = KeyRevoked
			t := time.Now()
			k.RevokedAt = &t
			k.RevokedBy = revokedBy
			k.RevokeReason = reason
		}
	}
	km.mu.Unlock()

	fmt.Printf("[KeyManager] Revoked key %s by %s — reason: %s\n", keyOrID, revokedBy, reason)
	return nil
}

// Suspend temporarily disables a key (can be re-activated)
func (km *KeyManager) Suspend(keyID int, by string) error {
	if km.db == nil {
		return fmt.Errorf("database not available")
	}

	_, err := km.db.Exec(`UPDATE api_keys SET status='suspended' WHERE id=?`, keyID)
	if err != nil {
		return fmt.Errorf("failed to suspend key: %w", err)
	}

	km.mu.Lock()
	for _, k := range km.cache {
		if k.ID == keyID {
			k.Status = KeySuspended
		}
	}
	km.mu.Unlock()

	fmt.Printf("[KeyManager] Suspended key id=%d by %s\n", keyID, by)
	return nil
}

// Activate re-enables a suspended key
func (km *KeyManager) Activate(keyID int, by string) error {
	if km.db == nil {
		return fmt.Errorf("database not available")
	}

	_, err := km.db.Exec(`
		UPDATE api_keys SET status='active' WHERE id=? AND status='suspended'`, keyID)
	if err != nil {
		return fmt.Errorf("failed to activate key: %w", err)
	}

	km.mu.Lock()
	for _, k := range km.cache {
		if k.ID == keyID {
			k.Status = KeyActive
		}
	}
	km.mu.Unlock()

	fmt.Printf("[KeyManager] Activated key id=%d by %s\n", keyID, by)
	return nil
}

// GetAll returns all API keys with optional status filter
func (km *KeyManager) GetAll(statusFilter string) ([]APIKey, error) {
	if km.db == nil {
		return []APIKey{}, nil // no DB in tests — return empty list, not error
	}
	query := `
		SELECT id, key, name, owner, status, rate_limit,
		       created_at, last_used_at, expires_at,
		       revoked_at, revoked_by, revoke_reason,
		       request_count, block_count
		FROM api_keys`

	args := []interface{}{}
	if statusFilter != "" {
		query += ` WHERE status = ?`
		args = append(args, statusFilter)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := km.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var k APIKey
		var lastUsed, expires, revokedAt sql.NullString
		var createdStr string

		if err := rows.Scan(
			&k.ID, &k.Key, &k.Name, &k.Owner, &k.Status, &k.RateLimit,
			&createdStr, &lastUsed, &expires,
			&revokedAt, &k.RevokedBy, &k.RevokeReason,
			&k.RequestCount, &k.BlockCount,
		); err != nil {
			continue
		}

		k.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		if lastUsed.Valid && lastUsed.String != "" {
			t, _ := time.Parse("2006-01-02 15:04:05", lastUsed.String)
			k.LastUsedAt = &t
		}
		if expires.Valid && expires.String != "" {
			t, _ := time.Parse("2006-01-02 15:04:05", expires.String)
			k.ExpiresAt = &t
		}
		if revokedAt.Valid && revokedAt.String != "" {
			t, _ := time.Parse("2006-01-02 15:04:05", revokedAt.String)
			k.RevokedAt = &t
		}

		// Mask the key in list view — show only prefix + last 4 chars
		if len(k.Key) > 10 {
			k.Key = k.Key[:7] + "..." + k.Key[len(k.Key)-4:]
		}

		keys = append(keys, k)
	}
	return keys, nil
}

// Stats returns key counts by status
func (km *KeyManager) Stats() map[string]int {
	stats := map[string]int{
		"active": 0, "revoked": 0, "suspended": 0, "expired": 0, "total": 0,
	}
	if km.db == nil {
		return stats
	}

	rows, err := km.db.Query(`SELECT status, COUNT(*) FROM api_keys GROUP BY status`)
	if err != nil {
		return stats
	}
	defer rows.Close()

	total := 0
	for rows.Next() {
		var status string
		var count int
		if rows.Scan(&status, &count) == nil {
			stats[status] = count
			total += count
		}
	}
	stats["total"] = total
	return stats
}

// RecordBlock increments the block count for a key
func (km *KeyManager) RecordBlock(rawKey string) {
	if km.db == nil {
		return
	}
	km.db.Exec(`UPDATE api_keys SET block_count = block_count + 1 WHERE key = ?`, rawKey)
	km.mu.Lock()
	if k, ok := km.cache[rawKey]; ok {
		k.BlockCount++
	}
	km.mu.Unlock()
}

// recordUsage updates last_used_at and increments request_count
func (km *KeyManager) recordUsage(rawKey string, blocked bool) {
	if km.db == nil {
		return
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	km.db.Exec(`
		UPDATE api_keys SET last_used_at=?, request_count=request_count+1
		WHERE key=?`, now, rawKey)

	km.mu.Lock()
	if k, ok := km.cache[rawKey]; ok {
		t := time.Now()
		k.LastUsedAt = &t
		k.RequestCount++
	}
	km.mu.Unlock()
}

// markExpired marks a key as expired in DB and cache
func (km *KeyManager) markExpired(rawKey string) {
	if km.db == nil {
		return
	}
	km.db.Exec(`UPDATE api_keys SET status='expired' WHERE key=?`, rawKey)
	km.mu.Lock()
	if k, ok := km.cache[rawKey]; ok {
		k.Status = KeyExpired
	}
	km.mu.Unlock()
}

// GetRateLimit returns the per-minute rate limit for a key
// 0 means use the system default
func (km *KeyManager) GetRateLimit(rawKey string) int {
	km.mu.RLock()
	defer km.mu.RUnlock()
	if k, ok := km.cache[rawKey]; ok {
		return k.RateLimit
	}
	return 0
}
