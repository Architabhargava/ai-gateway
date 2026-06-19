package compliance

import (
	"database/sql"
	"fmt"
	"time"
)

// ReviewStatus represents the current state of a queued request
type ReviewStatus string

const (
	ReviewPending  ReviewStatus = "pending"
	ReviewApproved ReviewStatus = "approved"
	ReviewRejected ReviewStatus = "rejected"
	ReviewExpired  ReviewStatus = "expired"
)

// ReviewItem represents a single request held in the human review queue
type ReviewItem struct {
	ID         int          `json:"id"`
	AuditLogID int          `json:"audit_log_id"`
	Prompt     string       `json:"prompt"`
	ClientIP   string       `json:"client_ip"`
	Score      float64      `json:"score"`
	Category   string       `json:"category"`
	Reasoning  string       `json:"reasoning"`
	Status     ReviewStatus `json:"status"`
	ExpiresAt  time.Time    `json:"expires_at"`
	DecidedAt  *time.Time   `json:"decided_at,omitempty"`
	Reviewer   string       `json:"reviewer,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
}

// ReviewQueue manages the human review queue for borderline AI decisions
type ReviewQueue struct {
	db             *sql.DB
	timeoutSeconds int
}

// NewReviewQueue creates and initialises the review queue
func NewReviewQueue(db *sql.DB) *ReviewQueue {
	if db == nil {
		fmt.Println("[ReviewQueue] No database — review queue disabled")
		return &ReviewQueue{timeoutSeconds: 300}
	}

	q := &ReviewQueue{
		db:             db,
		timeoutSeconds: 300, // 5 minutes — enough time to open dashboard and decide
	}

	if err := q.initDB(); err != nil {
		fmt.Println("[ReviewQueue] Failed to initialise table:", err)
		return q
	}

	// Background goroutine marks expired items every 10 seconds
	go q.expireLoop()

	fmt.Printf("[ReviewQueue] Initialised — timeout: %ds\n", q.timeoutSeconds)
	return q
}

// initDB creates the review_queue table
func (q *ReviewQueue) initDB() error {
	_, err := q.db.Exec(`
		CREATE TABLE IF NOT EXISTS review_queue (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			audit_log_id INTEGER NOT NULL DEFAULT 0,
			prompt       TEXT    NOT NULL DEFAULT '',
			client_ip    TEXT    NOT NULL DEFAULT '',
			score        REAL    NOT NULL DEFAULT 0.0,
			category     TEXT    NOT NULL DEFAULT '',
			reasoning    TEXT    NOT NULL DEFAULT '',
			status       TEXT    NOT NULL DEFAULT 'pending',
			expires_at   DATETIME NOT NULL,
			decided_at   DATETIME,
			reviewer     TEXT    NOT NULL DEFAULT '',
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
		)`)
	if err != nil {
		return fmt.Errorf("failed to create review_queue table: %w", err)
	}

	count := 0
	q.db.QueryRow(`SELECT COUNT(*) FROM review_queue WHERE status = 'pending'`).Scan(&count)
	fmt.Printf("[ReviewQueue] Table ready — %d pending items\n", count)
	return nil
}

// Enqueue adds a borderline request to the review queue
func (q *ReviewQueue) Enqueue(auditLogID int, prompt, clientIP, category, reasoning string, score float64) (int, error) {
	if q.db == nil {
		return 0, fmt.Errorf("review queue database not available")
	}

	expiresAt := time.Now().Add(time.Duration(q.timeoutSeconds) * time.Second)

	result, err := q.db.Exec(`
		INSERT INTO review_queue
			(audit_log_id, prompt, client_ip, score, category, reasoning, status, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)`,
		auditLogID, prompt, clientIP, score, category, reasoning,
		expiresAt.Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to enqueue review item: %w", err)
	}

	id, _ := result.LastInsertId()
	fmt.Printf("[ReviewQueue] Enqueued id=%d score=%.2f category=%s expires=%s\n",
		id, score, category, expiresAt.Format("15:04:05"))

	return int(id), nil
}

// Poll checks the database every second until a decision is made or expiry.
// Uses pure DB polling — no in-memory channels — so the decision written
// by HandleReview is always visible regardless of goroutine scheduling.
func (q *ReviewQueue) Poll(itemID int) ReviewStatus {
	if q.db == nil {
		return ReviewExpired
	}

	fmt.Printf("[ReviewQueue] Polling for decision on id=%d\n", itemID)

	deadline := time.Now().Add(time.Duration(q.timeoutSeconds) * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
			var status string
			var expiresStr string

			err := q.db.QueryRow(
				`SELECT status, expires_at FROM review_queue WHERE id = ?`, itemID,
			).Scan(&status, &expiresStr)

			if err != nil {
				fmt.Printf("[ReviewQueue] Poll DB error id=%d: %v\n", itemID, err)
				return ReviewExpired
			}

			fmt.Printf("[ReviewQueue] Poll tick id=%d status=%s\n", itemID, status)

			switch ReviewStatus(status) {
			case ReviewApproved:
				fmt.Printf("[ReviewQueue] Decision: APPROVED id=%d\n", itemID)
				return ReviewApproved
			case ReviewRejected:
				fmt.Printf("[ReviewQueue] Decision: REJECTED id=%d\n", itemID)
				return ReviewRejected
			case ReviewExpired:
				fmt.Printf("[ReviewQueue] Decision: EXPIRED id=%d\n", itemID)
				return ReviewExpired
			}

			// Still pending — check deadline
			if time.Now().After(deadline) {
				q.markExpired(itemID)
				fmt.Printf("[ReviewQueue] Deadline passed — marking expired id=%d\n", itemID)
				return ReviewExpired
			}
		}
	}
}

// Decide sets the decision on a review item — called by the admin API
func (q *ReviewQueue) Decide(itemID int, status ReviewStatus, reviewer string) error {
	if q.db == nil {
		return fmt.Errorf("review queue database not available")
	}

	if status != ReviewApproved && status != ReviewRejected {
		return fmt.Errorf("invalid status %q — must be approved or rejected", status)
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	// First check current status
	var currentStatus string
	err := q.db.QueryRow(`SELECT status FROM review_queue WHERE id = ?`, itemID).Scan(&currentStatus)
	if err != nil {
		return fmt.Errorf("review item %d not found: %w", itemID, err)
	}

	if currentStatus != "pending" {
		return fmt.Errorf("review item %d is already %s — cannot change decision", itemID, currentStatus)
	}

	result, err := q.db.Exec(`
		UPDATE review_queue
		SET status = ?, decided_at = ?, reviewer = ?
		WHERE id = ? AND status = 'pending'`,
		string(status), now, reviewer, itemID,
	)
	if err != nil {
		return fmt.Errorf("failed to update review item %d: %w", itemID, err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("review item %d could not be updated — it may have just expired", itemID)
	}

	fmt.Printf("[ReviewQueue] Decision recorded id=%d status=%s reviewer=%s\n",
		itemID, status, reviewer)
	return nil
}

// GetPending returns all pending review items oldest first
func (q *ReviewQueue) GetPending() ([]ReviewItem, error) {
	if q.db == nil {
		return []ReviewItem{}, nil
	}

	rows, err := q.db.Query(`
		SELECT id, audit_log_id, prompt, client_ip, score, category,
		       reasoning, status, expires_at, decided_at, reviewer, created_at
		FROM review_queue
		WHERE status = 'pending'
		ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed to query review queue: %w", err)
	}
	defer rows.Close()
	return q.scanItems(rows)
}

// GetAll returns all review items newest first
func (q *ReviewQueue) GetAll() ([]ReviewItem, error) {
	if q.db == nil {
		return []ReviewItem{}, nil
	}

	rows, err := q.db.Query(`
		SELECT id, audit_log_id, prompt, client_ip, score, category,
		       reasoning, status, expires_at, decided_at, reviewer, created_at
		FROM review_queue
		ORDER BY created_at DESC
		LIMIT 100`)
	if err != nil {
		return nil, fmt.Errorf("failed to query review queue: %w", err)
	}
	defer rows.Close()
	return q.scanItems(rows)
}

// scanItems scans SQL rows into ReviewItem structs
func (q *ReviewQueue) scanItems(rows *sql.Rows) ([]ReviewItem, error) {
	var items []ReviewItem
	for rows.Next() {
		var item ReviewItem
		var expiresStr, createdStr string
		var decidedStr sql.NullString
		var reviewer sql.NullString

		if err := rows.Scan(
			&item.ID, &item.AuditLogID, &item.Prompt, &item.ClientIP,
			&item.Score, &item.Category, &item.Reasoning, &item.Status,
			&expiresStr, &decidedStr, &reviewer, &createdStr,
		); err != nil {
			continue
		}

		item.ExpiresAt, _ = time.Parse("2006-01-02 15:04:05", expiresStr)
		item.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdStr)
		if decidedStr.Valid && decidedStr.String != "" {
			t, _ := time.Parse("2006-01-02 15:04:05", decidedStr.String)
			item.DecidedAt = &t
		}
		if reviewer.Valid {
			item.Reviewer = reviewer.String
		}

		items = append(items, item)
	}
	return items, nil
}

// markExpired sets a pending item's status to expired
func (q *ReviewQueue) markExpired(itemID int) {
	q.db.Exec(`
		UPDATE review_queue SET status = 'expired'
		WHERE id = ? AND status = 'pending'`, itemID)
}

// expireLoop marks overdue pending items as expired every 10 seconds
func (q *ReviewQueue) expireLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if q.db == nil {
			return
		}
		now := time.Now().Format("2006-01-02 15:04:05")
		result, err := q.db.Exec(`
			UPDATE review_queue
			SET status = 'expired'
			WHERE status = 'pending' AND expires_at < ?`, now)
		if err != nil {
			continue
		}
		if n, _ := result.RowsAffected(); n > 0 {
			fmt.Printf("[ReviewQueue] Expired %d overdue item(s)\n", n)
		}
	}
}

// Stats returns counts by status
func (q *ReviewQueue) Stats() map[string]int {
	stats := map[string]int{
		"pending":  0,
		"approved": 0,
		"rejected": 0,
		"expired":  0,
	}
	if q.db == nil {
		return stats
	}

	rows, err := q.db.Query(`SELECT status, COUNT(*) FROM review_queue GROUP BY status`)
	if err != nil {
		return stats
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if rows.Scan(&status, &count) == nil {
			stats[status] = count
		}
	}
	return stats
}

// truncate shortens a string for log output
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
