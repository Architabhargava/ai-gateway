package logger

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// RiskLevel maps to the EU AI Act four-tier risk classification
type RiskLevel string

const (
	RiskMinimal      RiskLevel = "minimal"
	RiskLimited      RiskLevel = "limited"
	RiskHigh         RiskLevel = "high"
	RiskUnacceptable RiskLevel = "unacceptable"
)

// AuditLog represents a single logged request with full EU AI Act compliance fields
type AuditLog struct {
	ID              int
	Timestamp       time.Time
	ClientIP        string
	Prompt          string
	Response        string
	Status          string
	Blocked         bool
	Reason          string
	ReasoningChain  string
	RiskLevel       RiskLevel
	EUArticle       string
	Category        string
	ClassifierScore float64
}

// Logger writes audit records to SQLite (local) or in-memory (cloud free tier)
type Logger struct {
	db        *sql.DB
	useMemory bool
	memory    []AuditLog
	counter   int
	mu        sync.Mutex
}

// New opens or creates the SQLite database and ensures all tables exist
func New(dbPath string) (*Logger, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Println("[Logger] SQLite unavailable — using in-memory logger")
		return &Logger{useMemory: true}, nil
	}

	if err := db.Ping(); err != nil {
		fmt.Println("[Logger] SQLite ping failed — using in-memory logger")
		return &Logger{useMemory: true}, nil
	}

	if err := migrate(db); err != nil {
		fmt.Printf("[Logger] Migration failed — using in-memory logger: %v\n", err)
		return &Logger{useMemory: true}, nil
	}

	fmt.Println("[Logger] SQLite ready at", dbPath)
	return &Logger{db: db}, nil
}

// migrate creates and upgrades all tables
func migrate(db *sql.DB) error {
	statements := []string{
		// Core audit log table with all EU AI Act compliance columns
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp        DATETIME DEFAULT CURRENT_TIMESTAMP,
			client_ip        TEXT    NOT NULL DEFAULT '',
			prompt           TEXT    NOT NULL DEFAULT '',
			response         TEXT    NOT NULL DEFAULT '',
			status           TEXT    NOT NULL DEFAULT '',
			blocked          INTEGER NOT NULL DEFAULT 0,
			reason           TEXT    NOT NULL DEFAULT '',
			reasoning_chain  TEXT    NOT NULL DEFAULT '',
			risk_level       TEXT    NOT NULL DEFAULT 'minimal',
			eu_article       TEXT    NOT NULL DEFAULT '',
			category         TEXT    NOT NULL DEFAULT '',
			classifier_score REAL    NOT NULL DEFAULT 0.0
		)`,

		// Add new columns to existing databases that were created before this migration
		// SQLite does not support IF NOT EXISTS on ALTER TABLE so we ignore errors
		`ALTER TABLE audit_logs ADD COLUMN reasoning_chain  TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE audit_logs ADD COLUMN risk_level       TEXT NOT NULL DEFAULT 'minimal'`,
		`ALTER TABLE audit_logs ADD COLUMN eu_article       TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE audit_logs ADD COLUMN category         TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE audit_logs ADD COLUMN classifier_score REAL NOT NULL DEFAULT 0.0`,

		// Blocked keyword rules managed via admin API
		`CREATE TABLE IF NOT EXISTS blocked_rules (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			word     TEXT UNIQUE NOT NULL COLLATE NOCASE,
			added_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			// ALTER TABLE errors are expected on fresh DBs — ignore duplicate column errors
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("migration failed on [%.60s...]: %w", stmt, err)
			}
		}
	}

	fmt.Println("[Logger] Database schema up to date")
	return nil
}

// isDuplicateColumnError checks if an error is a SQLite duplicate column error
// which happens when ALTER TABLE tries to add a column that already exists
func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "duplicate column") ||
		contains(msg, "already exists") ||
		contains(msg, "no such column") ||
		contains(msg, "UNIQUE constraint")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// DB returns the underlying *sql.DB for shared use by other packages
func (l *Logger) DB() *sql.DB {
	if l.useMemory {
		return nil
	}
	return l.db
}

// LogEntry holds all fields for a single audit log write
type LogEntry struct {
	ClientIP        string
	Prompt          string
	Response        string
	Status          string
	Blocked         bool
	Reason          string
	ReasoningChain  string
	RiskLevel       RiskLevel
	EUArticle       string
	Category        string
	ClassifierScore float64
}

// Log writes a single audit record with full EU AI Act compliance fields
func (l *Logger) Log(entry LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.useMemory {
		l.counter++
		record := AuditLog{
			ID:              l.counter,
			Timestamp:       time.Now(),
			ClientIP:        entry.ClientIP,
			Prompt:          entry.Prompt,
			Response:        entry.Response,
			Status:          entry.Status,
			Blocked:         entry.Blocked,
			Reason:          entry.Reason,
			ReasoningChain:  entry.ReasoningChain,
			RiskLevel:       entry.RiskLevel,
			EUArticle:       entry.EUArticle,
			Category:        entry.Category,
			ClassifierScore: entry.ClassifierScore,
		}
		l.memory = append([]AuditLog{record}, l.memory...)
		if len(l.memory) > 200 {
			l.memory = l.memory[:200]
		}
		fmt.Printf("[Logger] (memory) %s | %s | risk=%s | blocked=%v\n",
			entry.ClientIP, entry.Status, entry.RiskLevel, entry.Blocked)
		return nil
	}

	blockedInt := 0
	if entry.Blocked {
		blockedInt = 1
	}

	_, err := l.db.Exec(`
		INSERT INTO audit_logs
			(client_ip, prompt, response, status, blocked, reason,
			 reasoning_chain, risk_level, eu_article, category, classifier_score)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ClientIP,
		entry.Prompt,
		entry.Response,
		entry.Status,
		blockedInt,
		entry.Reason,
		entry.ReasoningChain,
		string(entry.RiskLevel),
		entry.EUArticle,
		entry.Category,
		entry.ClassifierScore,
	)
	if err != nil {
		return fmt.Errorf("failed to write audit log: %w", err)
	}

	fmt.Printf("[Logger] (sqlite) %s | %s | risk=%s | score=%.2f | blocked=%v\n",
		entry.ClientIP, entry.Status, entry.RiskLevel, entry.ClassifierScore, entry.Blocked)
	return nil
}

// GetAll returns up to 200 most recent audit records newest first
func (l *Logger) GetAll() ([]AuditLog, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.useMemory {
		out := make([]AuditLog, len(l.memory))
		copy(out, l.memory)
		return out, nil
	}

	rows, err := l.db.Query(`
		SELECT id, timestamp, client_ip, prompt, response, status,
		       blocked, reason, reasoning_chain, risk_level, eu_article,
		       category, classifier_score
		FROM audit_logs
		ORDER BY id DESC
		LIMIT 200`)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var entry AuditLog
		var blocked int
		var ts, riskLevel string
		if err := rows.Scan(
			&entry.ID, &ts, &entry.ClientIP, &entry.Prompt,
			&entry.Response, &entry.Status, &blocked, &entry.Reason,
			&entry.ReasoningChain, &riskLevel, &entry.EUArticle,
			&entry.Category, &entry.ClassifierScore,
		); err != nil {
			continue
		}
		entry.Blocked = blocked == 1
		entry.RiskLevel = RiskLevel(riskLevel)
		entry.Timestamp, _ = time.Parse("2006-01-02 15:04:05", ts)
		logs = append(logs, entry)
	}
	return logs, nil
}

// GetByID returns a single audit log entry by ID for detailed inspection
func (l *Logger) GetByID(id int) (*AuditLog, error) {
	if l.useMemory {
		l.mu.Lock()
		defer l.mu.Unlock()
		for _, entry := range l.memory {
			if entry.ID == id {
				e := entry
				return &e, nil
			}
		}
		return nil, fmt.Errorf("log entry %d not found", id)
	}

	row := l.db.QueryRow(`
		SELECT id, timestamp, client_ip, prompt, response, status,
		       blocked, reason, reasoning_chain, risk_level, eu_article,
		       category, classifier_score
		FROM audit_logs WHERE id = ?`, id)

	var entry AuditLog
	var blocked int
	var ts, riskLevel string
	if err := row.Scan(
		&entry.ID, &ts, &entry.ClientIP, &entry.Prompt,
		&entry.Response, &entry.Status, &blocked, &entry.Reason,
		&entry.ReasoningChain, &riskLevel, &entry.EUArticle,
		&entry.Category, &entry.ClassifierScore,
	); err != nil {
		return nil, fmt.Errorf("log entry %d not found: %w", id, err)
	}
	entry.Blocked = blocked == 1
	entry.RiskLevel = RiskLevel(riskLevel)
	entry.Timestamp, _ = time.Parse("2006-01-02 15:04:05", ts)
	return &entry, nil
}

// Close shuts down the database connection cleanly
func (l *Logger) Close() {
	if l.db != nil {
		l.db.Close()
	}
}
