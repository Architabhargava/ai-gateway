package logger

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type AuditLog struct {
	ID        int
	Timestamp time.Time
	ClientIP  string
	Prompt    string
	Response  string
	Status    string
	Blocked   bool
	Reason    string
}

type Logger struct {
	db        *sql.DB
	memory    []AuditLog
	useMemory bool
	mu        sync.Mutex
	counter   int
}

func New(dbPath string) (*Logger, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Println("[Logger] SQLite unavailable, using in-memory logger")
		return &Logger{useMemory: true}, nil
	}

	err = createTable(db)
	if err != nil {
		fmt.Println("[Logger] SQLite table error, using in-memory logger")
		return &Logger{useMemory: true}, nil
	}

	fmt.Println("[Logger] SQLite database ready at", dbPath)
	return &Logger{db: db}, nil
}

func createTable(db *sql.DB) error {
	query := `
	CREATE TABLE IF NOT EXISTS audit_logs (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		client_ip TEXT,
		prompt    TEXT,
		response  TEXT,
		status    TEXT,
		blocked   INTEGER DEFAULT 0,
		reason    TEXT
	);`
	_, err := db.Exec(query)
	return err
}

func (l *Logger) Log(clientIP, prompt, response, status string, blocked bool, reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.useMemory {
		l.counter++
		l.memory = append([]AuditLog{{
			ID:        l.counter,
			Timestamp: time.Now(),
			ClientIP:  clientIP,
			Prompt:    prompt,
			Response:  response,
			Status:    status,
			Blocked:   blocked,
			Reason:    reason,
		}}, l.memory...)
		if len(l.memory) > 100 {
			l.memory = l.memory[:100]
		}
		fmt.Printf("[Logger] Saved (memory) — IP: %s | Status: %s | Blocked: %v\n", clientIP, status, blocked)
		return nil
	}

	query := `
	INSERT INTO audit_logs (client_ip, prompt, response, status, blocked, reason)
	VALUES (?, ?, ?, ?, ?, ?)`

	blockedInt := 0
	if blocked {
		blockedInt = 1
	}

	_, err := l.db.Exec(query, clientIP, prompt, response, status, blockedInt, reason)
	if err != nil {
		return fmt.Errorf("failed to log request: %w", err)
	}

	fmt.Printf("[Logger] Saved — IP: %s | Status: %s | Blocked: %v\n", clientIP, status, blocked)
	return nil
}

func (l *Logger) GetAll() ([]AuditLog, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.useMemory {
		return l.memory, nil
	}

	query := `SELECT id, timestamp, client_ip, prompt, response, status, blocked, reason
	          FROM audit_logs ORDER BY timestamp DESC LIMIT 100`

	rows, err := l.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var log AuditLog
		var blockedInt int
		var ts string
		err := rows.Scan(&log.ID, &ts, &log.ClientIP, &log.Prompt,
			&log.Response, &log.Status, &blockedInt, &log.Reason)
		if err != nil {
			continue
		}
		log.Blocked = blockedInt == 1
		log.Timestamp, _ = time.Parse("2006-01-02 15:04:05", ts)
		logs = append(logs, log)
	}
	return logs, nil
}

func (l *Logger) Close() {
	if l.db != nil {
		l.db.Close()
	}
}
