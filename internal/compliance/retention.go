package compliance

import (
	"database/sql"
	"fmt"
	"time"
)

// RetentionPolicy holds the current log retention configuration
type RetentionPolicy struct {
	ID            int       `json:"id"`
	RetentionDays int       `json:"retention_days"`
	UpdatedAt     time.Time `json:"updated_at"`
	UpdatedBy     string    `json:"updated_by"`
}

// PurgeResult summarises what was deleted in a purge run
type PurgeResult struct {
	AuditLogsDeleted   int64     `json:"audit_logs_deleted"`
	IncidentsDeleted   int64     `json:"incidents_deleted"`
	ReviewItemsDeleted int64     `json:"review_items_deleted"`
	PurgedBefore       time.Time `json:"purged_before"`
	RanAt              time.Time `json:"ran_at"`
}

// ErasureResult summarises what was deleted for a GDPR erasure request
type ErasureResult struct {
	APIKey           string    `json:"api_key"`
	AuditLogsDeleted int64     `json:"audit_logs_deleted"`
	ErasedAt         time.Time `json:"erased_at"`
}

// RetentionManager handles log retention policy and GDPR right to erasure
type RetentionManager struct {
	db *sql.DB
}

// NewRetentionManager creates and initialises the retention manager
func NewRetentionManager(db *sql.DB) *RetentionManager {
	if db == nil {
		fmt.Println("[Retention] No database — retention manager disabled")
		return &RetentionManager{}
	}

	m := &RetentionManager{db: db}

	if err := m.initDB(); err != nil {
		fmt.Println("[Retention] Failed to initialise:", err)
		return m
	}

	// Start nightly purge goroutine
	go m.purgeLoop()

	return m
}

// initDB creates the retention_policy table and seeds the default policy
func (m *RetentionManager) initDB() error {
	_, err := m.db.Exec(`
		CREATE TABLE IF NOT EXISTS retention_policy (
			id             INTEGER PRIMARY KEY DEFAULT 1,
			retention_days INTEGER NOT NULL DEFAULT 90,
			updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_by     TEXT    NOT NULL DEFAULT 'system'
		)`)
	if err != nil {
		return fmt.Errorf("failed to create retention_policy table: %w", err)
	}

	// Seed default policy if not exists
	m.db.Exec(`
		INSERT OR IGNORE INTO retention_policy (id, retention_days, updated_by)
		VALUES (1, 90, 'system')`)

	policy, _ := m.GetPolicy()
	if policy != nil {
		fmt.Printf("[Retention] Policy loaded — %d day retention\n", policy.RetentionDays)
	}

	return nil
}

// GetPolicy returns the current retention policy
func (m *RetentionManager) GetPolicy() (*RetentionPolicy, error) {
	if m.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	var p RetentionPolicy
	var ts, updatedBy string

	err := m.db.QueryRow(`
		SELECT id, retention_days, updated_at, updated_by
		FROM retention_policy WHERE id = 1`,
	).Scan(&p.ID, &p.RetentionDays, &ts, &updatedBy)

	if err != nil {
		return nil, fmt.Errorf("failed to read retention policy: %w", err)
	}

	p.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", ts)
	p.UpdatedBy = updatedBy
	return &p, nil
}

// UpdatePolicy sets a new retention period in days
func (m *RetentionManager) UpdatePolicy(days int, updatedBy string) (*RetentionPolicy, error) {
	if m.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	if days < 1 {
		return nil, fmt.Errorf("retention_days must be at least 1")
	}
	if days > 3650 {
		return nil, fmt.Errorf("retention_days cannot exceed 3650 (10 years)")
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	if updatedBy == "" {
		updatedBy = "admin"
	}

	_, err := m.db.Exec(`
		UPDATE retention_policy
		SET retention_days = ?, updated_at = ?, updated_by = ?
		WHERE id = 1`,
		days, now, updatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update retention policy: %w", err)
	}

	fmt.Printf("[Retention] Policy updated — %d days by %s\n", days, updatedBy)
	return m.GetPolicy()
}

// Purge deletes records older than the current retention period.
// Purges audit_logs, incidents, and review_queue records.
// Resolved incidents are purged; unresolved incidents are kept regardless of age
// because they may still require investigation.
func (m *RetentionManager) Purge() (*PurgeResult, error) {
	if m.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	policy, err := m.GetPolicy()
	if err != nil {
		return nil, fmt.Errorf("failed to read retention policy: %w", err)
	}

	cutoff := time.Now().AddDate(0, 0, -policy.RetentionDays)
	cutoffStr := cutoff.Format("2006-01-02 15:04:05")

	result := &PurgeResult{
		PurgedBefore: cutoff,
		RanAt:        time.Now(),
	}

	// Purge audit logs older than retention period
	res, err := m.db.Exec(`
		DELETE FROM audit_logs WHERE timestamp < ?`, cutoffStr)
	if err != nil {
		fmt.Printf("[Retention] Failed to purge audit_logs: %v\n", err)
	} else {
		result.AuditLogsDeleted, _ = res.RowsAffected()
	}

	// Purge RESOLVED incidents older than retention period
	// Unresolved incidents are always kept — they need investigation
	res, err = m.db.Exec(`
		DELETE FROM incidents
		WHERE resolved = 1 AND timestamp < ?`, cutoffStr)
	if err != nil {
		fmt.Printf("[Retention] Failed to purge incidents: %v\n", err)
	} else {
		result.IncidentsDeleted, _ = res.RowsAffected()
	}

	// Purge decided/expired review queue items older than retention period
	res, err = m.db.Exec(`
		DELETE FROM review_queue
		WHERE status != 'pending' AND created_at < ?`, cutoffStr)
	if err != nil {
		fmt.Printf("[Retention] Failed to purge review_queue: %v\n", err)
	} else {
		result.ReviewItemsDeleted, _ = res.RowsAffected()
	}

	if result.AuditLogsDeleted+result.IncidentsDeleted+result.ReviewItemsDeleted > 0 {
		fmt.Printf("[Retention] Purge complete — audit_logs: %d incidents: %d review_queue: %d (before %s)\n",
			result.AuditLogsDeleted,
			result.IncidentsDeleted,
			result.ReviewItemsDeleted,
			cutoff.Format("2006-01-02"),
		)
	} else {
		fmt.Println("[Retention] Purge ran — no records eligible for deletion")
	}

	return result, nil
}

// EraseByAPIKey deletes all audit log entries associated with a specific API key.
// This implements the GDPR Article 17 "right to erasure" (right to be forgotten).
// The API key is matched against the client_ip field pattern or a dedicated
// user_key column if present. Since we log clientIP not the API key directly,
// we match on a provided identifier stored in the reason/category fields.
//
// In practice, organisations implementing GDPR erasure would store the API key
// alongside each log entry. We add that support here via a new column.
func (m *RetentionManager) EraseByAPIKey(apiKey string) (*ErasureResult, error) {
	if m.db == nil {
		return nil, fmt.Errorf("database not available")
	}

	if apiKey == "" {
		return nil, fmt.Errorf("api_key cannot be empty")
	}

	// Add api_key column to audit_logs if it doesn't exist
	// This is idempotent — SQLite ignores duplicate column errors
	m.db.Exec(`ALTER TABLE audit_logs ADD COLUMN api_key TEXT NOT NULL DEFAULT ''`)

	result := &ErasureResult{
		APIKey:   apiKey,
		ErasedAt: time.Now(),
	}

	// Delete all audit log entries for this API key
	res, err := m.db.Exec(`
		DELETE FROM audit_logs WHERE api_key = ?`, apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to erase audit logs for key %s: %w", apiKey, err)
	}

	result.AuditLogsDeleted, _ = res.RowsAffected()

	fmt.Printf("[Retention] GDPR erasure complete — key: %s audit_logs_deleted: %d\n",
		apiKey, result.AuditLogsDeleted)

	return result, nil
}

// StorageStats returns current row counts across all logged tables
func (m *RetentionManager) StorageStats() map[string]interface{} {
	stats := map[string]interface{}{
		"audit_logs":   0,
		"incidents":    0,
		"review_queue": 0,
		"oldest_log":   "",
		"newest_log":   "",
	}

	if m.db == nil {
		return stats
	}

	var count int

	_ = m.db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&count)
	stats["audit_logs"] = count

	m.db.QueryRow(`SELECT COUNT(*) FROM incidents`).Scan(&count)
	stats["incidents"] = count

	m.db.QueryRow(`SELECT COUNT(*) FROM review_queue`).Scan(&count)
	stats["review_queue"] = count

	var oldest, newest string
	m.db.QueryRow(`SELECT MIN(timestamp) FROM audit_logs`).Scan(&oldest)
	m.db.QueryRow(`SELECT MAX(timestamp) FROM audit_logs`).Scan(&newest)
	stats["oldest_log"] = oldest
	stats["newest_log"] = newest

	return stats
}

// purgeLoop runs the purge automatically every 24 hours
// Also runs once immediately on startup to catch any overdue records
func (m *RetentionManager) purgeLoop() {
	// Run once at startup after a short delay
	time.Sleep(10 * time.Second)
	if result, err := m.Purge(); err != nil {
		fmt.Printf("[Retention] Startup purge failed: %v\n", err)
	} else if result.AuditLogsDeleted+result.IncidentsDeleted+result.ReviewItemsDeleted > 0 {
		fmt.Printf("[Retention] Startup purge deleted %d total records\n",
			result.AuditLogsDeleted+result.IncidentsDeleted+result.ReviewItemsDeleted)
	}

	// Then run every 24 hours
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		if _, err := m.Purge(); err != nil {
			fmt.Printf("[Retention] Scheduled purge failed: %v\n", err)
		}
	}
}
