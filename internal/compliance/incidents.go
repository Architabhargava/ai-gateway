package compliance

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Severity levels for incidents — maps to EU AI Act risk classification
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// Incident represents a single security or compliance incident
type Incident struct {
	ID         int        `json:"id"`
	AuditLogID int        `json:"audit_log_id"`
	Timestamp  time.Time  `json:"timestamp"`
	Severity   Severity   `json:"severity"`
	Category   string     `json:"category"`
	EUArticle  string     `json:"eu_article"`
	Prompt     string     `json:"prompt"`
	ClientIP   string     `json:"client_ip"`
	Reason     string     `json:"reason"`
	EmailSent  bool       `json:"email_sent"`
	Resolved   bool       `json:"resolved"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

// IncidentManager handles incident creation, storage, and alerting
type IncidentManager struct {
	db        *sql.DB
	resendKey string
	emailTo   string
	emailFrom string
}

// NewIncidentManager creates and initialises the incident manager
func NewIncidentManager(db *sql.DB, resendKey, emailTo, emailFrom string) *IncidentManager {
	m := &IncidentManager{
		db:        db,
		resendKey: resendKey,
		emailTo:   emailTo,
		emailFrom: emailFrom,
	}

	if db != nil {
		m.initDB()
	}

	if resendKey != "" {
		fmt.Println("[Incidents] Resend email alerts enabled")
	} else {
		fmt.Println("[Incidents] No RESEND_API_KEY — email alerts disabled")
	}

	return m
}

// initDB creates the incidents table
func (m *IncidentManager) initDB() {
	_, err := m.db.Exec(`
		CREATE TABLE IF NOT EXISTS incidents (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			audit_log_id INTEGER NOT NULL DEFAULT 0,
			timestamp    DATETIME DEFAULT CURRENT_TIMESTAMP,
			severity     TEXT    NOT NULL DEFAULT 'low',
			category     TEXT    NOT NULL DEFAULT '',
			eu_article   TEXT    NOT NULL DEFAULT '',
			prompt       TEXT    NOT NULL DEFAULT '',
			client_ip    TEXT    NOT NULL DEFAULT '',
			reason       TEXT    NOT NULL DEFAULT '',
			email_sent   INTEGER NOT NULL DEFAULT 0,
			resolved     INTEGER NOT NULL DEFAULT 0,
			resolved_by  TEXT    NOT NULL DEFAULT '',
			resolved_at  DATETIME
		)`)
	if err != nil {
		fmt.Println("[Incidents] Failed to create incidents table:", err)
		return
	}

	count := 0
	_ = m.db.QueryRow(`SELECT COUNT(*) FROM incidents WHERE resolved = 0`).Scan(&count)
	fmt.Printf("[Incidents] Table ready — %d unresolved incidents\n", count)
}

// DetermineServerity maps risk level and category to an incident severity
func DetermineSeverity(riskLevel, category, euArticle string) Severity {
	// Article 5 violations are always critical — legally prohibited
	if strings.HasPrefix(euArticle, "Article 5") {
		return SeverityCritical
	}

	// Unacceptable risk = critical
	if riskLevel == "unacceptable" {
		return SeverityCritical
	}

	// High risk categories
	if riskLevel == "high" {
		switch category {
		case "jailbreak", "harmful_content":
			return SeverityHigh
		default:
			return SeverityMedium
		}
	}

	// Sensitive categories even at lower risk
	switch category {
	case "data_extraction", "prompt_injection":
		return SeverityMedium
	case "identity_manipulation":
		return SeverityLow
	}

	return SeverityLow
}

// Create records a new incident and fires an email alert if severity warrants it
func (m *IncidentManager) Create(
	auditLogID int,
	severity Severity,
	category, euArticle, prompt, clientIP, reason string,
) (*Incident, error) {
	if m.db == nil {
		return nil, fmt.Errorf("incidents database not available")
	}

	result, err := m.db.Exec(`
		INSERT INTO incidents
			(audit_log_id, severity, category, eu_article, prompt, client_ip, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		auditLogID, string(severity), category, euArticle, prompt, clientIP, reason,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create incident: %w", err)
	}

	id, _ := result.LastInsertId()
	incident := &Incident{
		ID:         int(id),
		AuditLogID: auditLogID,
		Timestamp:  time.Now(),
		Severity:   severity,
		Category:   category,
		EUArticle:  euArticle,
		Prompt:     prompt,
		ClientIP:   clientIP,
		Reason:     reason,
	}

	fmt.Printf("[Incidents] Created #%d — severity=%s category=%s article=%s\n",
		id, severity, category, euArticle)

	// Send email alert for medium, high, and critical incidents
	if severity == SeverityMedium || severity == SeverityHigh || severity == SeverityCritical {
		go m.sendEmailAlert(incident)
	}

	return incident, nil
}

// sendEmailAlert sends an incident alert via Resend API
func (m *IncidentManager) sendEmailAlert(incident *Incident) {
	if m.resendKey == "" || m.emailTo == "" {
		fmt.Println("[Incidents] Email alert skipped — RESEND_API_KEY or ALERT_EMAIL_TO not set")
		return
	}

	from := m.emailFrom
	if from == "" {
		from = "onboarding@resend.dev"
	}

	severityEmoji := map[Severity]string{
		SeverityLow:      "🟡",
		SeverityMedium:   "🟠",
		SeverityHigh:     "🔴",
		SeverityCritical: "🚨",
	}

	subject := fmt.Sprintf("%s AI Gateway Incident #%d — %s severity [%s]",
		severityEmoji[incident.Severity],
		incident.ID,
		strings.ToUpper(string(incident.Severity)),
		incident.Category,
	)

	// Truncate prompt for email display
	displayPrompt := incident.Prompt
	if len(displayPrompt) > 300 {
		displayPrompt = displayPrompt[:300] + "..."
	}

	articleSection := ""
	if incident.EUArticle != "" {
		articleSection = fmt.Sprintf(`
			<tr>
				<td style="padding:8px 0;color:#888;font-size:13px;width:140px">EU AI Act Article</td>
				<td style="padding:8px 0;font-size:13px"><strong style="color:#f87171">%s</strong></td>
			</tr>`, incident.EUArticle)
	}

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="margin:0;padding:0;background:#0f0f1a;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif">
<div style="max-width:600px;margin:0 auto;padding:32px 24px">

  <div style="background:#1a1a2e;border:1px solid #2a2a3e;border-radius:12px;overflow:hidden">

    <!-- Header -->
    <div style="background:%s;padding:20px 24px">
      <h1 style="margin:0;font-size:18px;font-weight:600;color:#fff">
        %s AI Gateway Security Incident
      </h1>
      <p style="margin:6px 0 0;font-size:13px;color:rgba(255,255,255,0.7)">
        Incident #%d &nbsp;·&nbsp; %s &nbsp;·&nbsp; %s
      </p>
    </div>

    <!-- Body -->
    <div style="padding:24px">
      <table style="width:100%%;border-collapse:collapse">
        <tr>
          <td style="padding:8px 0;color:#888;font-size:13px;width:140px">Severity</td>
          <td style="padding:8px 0;font-size:13px"><strong style="color:%s">%s</strong></td>
        </tr>
        <tr>
          <td style="padding:8px 0;color:#888;font-size:13px">Category</td>
          <td style="padding:8px 0;font-size:13px;color:#e0e0e0">%s</td>
        </tr>
        %s
        <tr>
          <td style="padding:8px 0;color:#888;font-size:13px">Client IP</td>
          <td style="padding:8px 0;font-size:13px;color:#e0e0e0;font-family:monospace">%s</td>
        </tr>
        <tr>
          <td style="padding:8px 0;color:#888;font-size:13px">Reason</td>
          <td style="padding:8px 0;font-size:13px;color:#e0e0e0">%s</td>
        </tr>
      </table>

      <!-- Prompt box -->
      <div style="margin-top:20px">
        <p style="margin:0 0 8px;font-size:12px;color:#666;text-transform:uppercase;letter-spacing:0.06em">Offending Prompt</p>
        <div style="background:#0f0f1a;border:1px solid #2a2a3e;border-radius:8px;padding:14px;font-size:13px;color:#ddd;line-height:1.6">
          %s
        </div>
      </div>

      <!-- Action button -->
      <div style="margin-top:24px;text-align:center">
        <a href="http://localhost:8080/admin/incidents"
           style="display:inline-block;padding:12px 28px;background:#4f46e5;color:#fff;text-decoration:none;border-radius:8px;font-size:14px;font-weight:500">
          View in Dashboard →
        </a>
      </div>
    </div>

    <!-- Footer -->
    <div style="padding:16px 24px;border-top:1px solid #2a2a3e;font-size:11px;color:#555;text-align:center">
      AI Gateway — EU AI Act Compliant &nbsp;·&nbsp; This is an automated security alert
    </div>

  </div>
</div>
</body>
</html>`,
		severityColor(incident.Severity),
		severityEmoji[incident.Severity],
		incident.ID,
		incident.Timestamp.Format("2006-01-02 15:04:05"),
		strings.ToUpper(string(incident.Severity)),
		severityColor(incident.Severity),
		strings.ToUpper(string(incident.Severity)),
		incident.Category,
		articleSection,
		incident.ClientIP,
		incident.Reason,
		displayPrompt,
	)

	payload := map[string]interface{}{
		"from":    from,
		"to":      []string{m.emailTo},
		"subject": subject,
		"html":    htmlBody,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		fmt.Println("[Incidents] Failed to marshal email payload:", err)
		return
	}

	req, err := http.NewRequest(
		http.MethodPost,
		"https://api.resend.com/emails",
		bytes.NewBuffer(bodyBytes),
	)
	if err != nil {
		fmt.Println("[Incidents] Failed to build email request:", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.resendKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("[Incidents] Email send failed:", err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		// Mark email as sent in DB
		m.db.Exec(`UPDATE incidents SET email_sent = 1 WHERE id = ?`, incident.ID)
		fmt.Printf("[Incidents] Email alert sent for incident #%d to %s\n",
			incident.ID, m.emailTo)
	} else {
		fmt.Printf("[Incidents] Email send failed HTTP %d: %s\n",
			resp.StatusCode, string(respBody))
	}
}

// severityColor returns the header background color for each severity
func severityColor(s Severity) string {
	switch s {
	case SeverityCritical:
		return "#7f1d1d"
	case SeverityHigh:
		return "#7c2d12"
	case SeverityMedium:
		return "#713f12"
	default:
		return "#1a1a2e"
	}
}

// GetAll returns all incidents newest first
func (m *IncidentManager) GetAll(severityFilter string) ([]Incident, error) {
	if m.db == nil {
		return []Incident{}, nil
	}

	query := `
		SELECT id, audit_log_id, timestamp, severity, category, eu_article,
		       prompt, client_ip, reason, email_sent, resolved, resolved_by, resolved_at
		FROM incidents`

	args := []interface{}{}
	if severityFilter != "" {
		query += ` WHERE severity = ?`
		args = append(args, severityFilter)
	}
	query += ` ORDER BY id DESC LIMIT 100`

	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query incidents: %w", err)
	}
	defer rows.Close()

	var incidents []Incident
	for rows.Next() {
		var inc Incident
		var ts, sev string
		var emailSent, resolved int
		var resolvedBy sql.NullString
		var resolvedAt sql.NullString

		if err := rows.Scan(
			&inc.ID, &inc.AuditLogID, &ts, &sev, &inc.Category,
			&inc.EUArticle, &inc.Prompt, &inc.ClientIP, &inc.Reason,
			&emailSent, &resolved, &resolvedBy, &resolvedAt,
		); err != nil {
			continue
		}

		inc.Timestamp, _ = time.Parse("2006-01-02 15:04:05", ts)
		inc.Severity = Severity(sev)
		inc.EmailSent = emailSent == 1
		inc.Resolved = resolved == 1
		if resolvedBy.Valid {
			inc.ResolvedBy = resolvedBy.String
		}
		if resolvedAt.Valid && resolvedAt.String != "" {
			t, _ := time.Parse("2006-01-02 15:04:05", resolvedAt.String)
			inc.ResolvedAt = &t
		}

		incidents = append(incidents, inc)
	}
	return incidents, nil
}

// Resolve marks an incident as resolved
func (m *IncidentManager) Resolve(id int, resolvedBy string) error {
	if m.db == nil {
		return fmt.Errorf("database not available")
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	result, err := m.db.Exec(`
		UPDATE incidents
		SET resolved = 1, resolved_by = ?, resolved_at = ?
		WHERE id = ? AND resolved = 0`,
		resolvedBy, now, id,
	)
	if err != nil {
		return fmt.Errorf("failed to resolve incident %d: %w", id, err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("incident %d not found or already resolved", id)
	}

	fmt.Printf("[Incidents] Resolved #%d by %s\n", id, resolvedBy)
	return nil
}

// Stats returns counts by severity and resolution status
func (m *IncidentManager) Stats() map[string]interface{} {
	stats := map[string]interface{}{
		"total":      0,
		"unresolved": 0,
		"critical":   0,
		"high":       0,
		"medium":     0,
		"low":        0,
	}

	if m.db == nil {
		return stats
	}

	var total, unresolved int
	_ = m.db.QueryRow(`SELECT COUNT(*) FROM incidents`).Scan(&total)
	_ = m.db.QueryRow(`SELECT COUNT(*) FROM incidents WHERE resolved = 0`).Scan(&unresolved)
	stats["total"] = total
	stats["unresolved"] = unresolved

	rows, err := m.db.Query(`SELECT severity, COUNT(*) FROM incidents GROUP BY severity`)
	if err != nil {
		return stats
	}
	defer rows.Close()

	for rows.Next() {
		var sev string
		var count int
		if rows.Scan(&sev, &count) == nil {
			stats[sev] = count
		}
	}
	return stats
}
