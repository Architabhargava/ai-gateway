package dashboard

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"ai-gateway/internal/logger"
)

type LogEntry struct {
	ID              int
	Timestamp       string
	ClientIP        string
	Prompt          string
	Response        string
	Status          string
	Blocked         bool
	Reason          string
	ReasoningChain  string
	RiskLevel       string
	EUArticle       string
	Category        string
	ClassifierScore float64
}

type Stats struct {
	Total        int
	Allowed      int
	Blocked      int
	Errors       int
	HighRisk     int
	Unacceptable int
}

type HourBucket struct {
	Hour  string
	Count int
}

type Dashboard struct {
	log *logger.Logger
}

func New(l *logger.Logger) *Dashboard {
	return &Dashboard{log: l}
}

// HandlePlatform serves the unified platform UI — all features in one page
func (d *Dashboard) HandlePlatform(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(platformHTML))
}

// HandleHome serves the chat UI (kept for /ai route compatibility)
func (d *Dashboard) HandleHome(w http.ResponseWriter, r *http.Request) {
	// Redirect to the platform
	http.Redirect(w, r, "/platform", http.StatusFound)
}

// HandleDashboard serves audit log data as JSON for the platform to consume
func (d *Dashboard) HandleDashboard(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	riskFilter := strings.TrimSpace(r.URL.Query().Get("risk"))

	allLogs, err := d.log.GetAll()
	if err != nil {
		http.Error(w, "Failed to load logs", http.StatusInternalServerError)
		return
	}

	var logs []LogEntry
	for _, l := range allLogs {
		if search != "" {
			sl := strings.ToLower(search)
			if !strings.Contains(strings.ToLower(l.Prompt), sl) &&
				!strings.Contains(strings.ToLower(l.Status), sl) &&
				!strings.Contains(strings.ToLower(l.ClientIP), sl) &&
				!strings.Contains(strings.ToLower(l.Category), sl) {
				continue
			}
		}
		if riskFilter != "" && string(l.RiskLevel) != riskFilter {
			continue
		}
		logs = append(logs, LogEntry{
			ID:              l.ID,
			Timestamp:       l.Timestamp.Format("2006-01-02 15:04:05"),
			ClientIP:        l.ClientIP,
			Prompt:          l.Prompt,
			Response:        l.Response,
			Status:          l.Status,
			Blocked:         l.Blocked,
			Reason:          l.Reason,
			ReasoningChain:  l.ReasoningChain,
			RiskLevel:       string(l.RiskLevel),
			EUArticle:       l.EUArticle,
			Category:        l.Category,
			ClassifierScore: l.ClassifierScore,
		})
	}

	stats := d.getStats(allLogs)
	buckets := d.getHourlyBuckets(allLogs)

	// If request accepts HTML, render dashboard page (legacy support)
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/html") {
		data := struct {
			Logs       []LogEntry
			Stats      Stats
			Buckets    []HourBucket
			Timestamp  string
			Search     string
			RiskFilter string
		}{logs, stats, buckets, time.Now().Format("2006-01-02 15:04:05"), search, riskFilter}

		tmpl, err := template.New("dashboard").Parse(legacyDashboardHTML)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		tmpl.Execute(w, data)
		return
	}

	// Default: return JSON for the platform UI to consume
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"logs":    logs,
		"stats":   stats,
		"buckets": buckets,
	})
}

func (d *Dashboard) HandleStats(w http.ResponseWriter, r *http.Request) {
	logs, err := d.log.GetAll()
	if err != nil {
		http.Error(w, "Failed to load stats", http.StatusInternalServerError)
		return
	}
	stats := d.getStats(logs)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (d *Dashboard) HandleReviewPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/platform#review", http.StatusFound)
}

func (d *Dashboard) HandleIncidentsPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/platform#incidents", http.StatusFound)
}

func (d *Dashboard) HandleRetentionPage(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/platform#retention", http.StatusFound)
}

func (d *Dashboard) HandleNotFound(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "404 — not found")
}

func (d *Dashboard) getStats(logs []logger.AuditLog) Stats {
	stats := Stats{Total: len(logs)}
	for _, l := range logs {
		switch l.Status {
		case "allowed":
			stats.Allowed++
		case "blocked":
			stats.Blocked++
		case "error":
			stats.Errors++
		}
		switch l.RiskLevel {
		case logger.RiskHigh:
			stats.HighRisk++
		case logger.RiskUnacceptable:
			stats.Unacceptable++
		}
	}
	return stats
}

func (d *Dashboard) getHourlyBuckets(logs []logger.AuditLog) []HourBucket {
	counts := map[string]int{}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, l := range logs {
		if l.Timestamp.After(cutoff) {
			hour := l.Timestamp.Format("15:00")
			counts[hour]++
		}
	}
	var buckets []HourBucket
	for h, c := range counts {
		buckets = append(buckets, HourBucket{Hour: h, Count: c})
	}
	return buckets
}

const legacyDashboardHTML = `<!DOCTYPE html>
<html><head><meta charset="UTF-8"><title>Dashboard</title></head>
<body><script>window.location.href='/platform'</script>
<p>Redirecting to <a href="/platform">the platform</a>...</p>
</body></html>`

const platformHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AI Gateway Platform</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#0a0a12;color:#e0e0e0;height:100vh;display:flex;overflow:hidden}

/* ── Sidebar ── */
.sidebar{width:220px;background:#111119;border-right:1px solid #1e1e2e;display:flex;flex-direction:column;flex-shrink:0}
.sidebar-logo{padding:18px 16px 14px;border-bottom:1px solid #1e1e2e}
.sidebar-logo h1{font-size:14px;font-weight:500;color:#fff;margin-bottom:2px}
.sidebar-logo p{font-size:11px;color:#555}
.eu-pill{display:inline-block;background:#1a2a4e;color:#93c5fd;border-radius:4px;padding:1px 7px;font-size:10px;margin-top:5px}
.nav{padding:10px 8px;flex:1;overflow-y:auto}
.nav-section{font-size:10px;font-weight:500;text-transform:uppercase;letter-spacing:0.08em;color:#444;padding:12px 8px 6px}
.nav-item{display:flex;align-items:center;gap:10px;padding:8px 10px;border-radius:8px;cursor:pointer;font-size:13px;color:#777;border:none;background:none;width:100%;text-align:left;transition:background 0.1s,color 0.1s}
.nav-item:hover{background:#1a1a28;color:#ccc}
.nav-item.active{background:#1e1e40;color:#fff;font-weight:500}
.nav-item.active .nav-icon{color:#6366f1}
.nav-icon{font-size:16px;flex-shrink:0;color:#444}
.nav-badge{margin-left:auto;background:#dc2626;color:#fff;border-radius:10px;font-size:10px;padding:1px 6px;font-weight:500}
.nav-badge.warn{background:#d97706}
.sidebar-footer{padding:12px 16px;border-top:1px solid #1e1e2e;font-size:11px;color:#444}
.status-dot{display:inline-block;width:6px;height:6px;border-radius:50%;background:#16a34a;margin-right:6px;animation:pulse 2s infinite}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:0.4}}

/* ── Main area ── */
.main{flex:1;display:flex;flex-direction:column;overflow:hidden}
.topbar{background:#111119;border-bottom:1px solid #1e1e2e;padding:12px 24px;display:flex;align-items:center;justify-content:space-between;flex-shrink:0}
.topbar-title{font-size:15px;font-weight:500;color:#fff}
.topbar-right{display:flex;gap:10px;align-items:center}
.key-input{background:#1a1a28;border:1px solid #2a2a3e;border-radius:6px;padding:6px 12px;font-size:12px;color:#ccc;width:190px;outline:none;font-family:monospace}
.key-input:focus{border-color:#6366f1}
.content{flex:1;overflow-y:auto}

/* ── Pages ── */
.page{display:none;height:100%}
.page.active{display:flex;flex-direction:column;height:100%}

/* ── Chat page ── */
.chat-layout{display:flex;flex:1;overflow:hidden}
.chat-history{width:220px;background:#0d0d18;border-right:1px solid #1e1e2e;padding:12px;overflow-y:auto;flex-shrink:0}
.chat-history-title{font-size:10px;text-transform:uppercase;letter-spacing:0.08em;color:#444;margin-bottom:10px;padding:0 4px}
.history-item{padding:8px 10px;border-radius:6px;cursor:pointer;margin-bottom:4px;border:1px solid transparent}
.history-item:hover{background:#1a1a28;border-color:#2a2a3e}
.history-item.active{background:#1e1e40;border-color:#4f46e5}
.h-prompt{font-size:12px;color:#bbb;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.h-meta{font-size:10px;color:#444;margin-top:2px;display:flex;gap:5px}
.b-sm{display:inline-block;padding:1px 6px;border-radius:8px;font-size:10px;font-weight:500}
.b-sm.allowed{background:#14532d;color:#4ade80}
.b-sm.blocked{background:#450a0a;color:#f87171}
.b-sm.error,.b-sm.review_pending{background:#3a2a00;color:#fbbf24}
.r-sm{display:inline-block;padding:1px 5px;border-radius:8px;font-size:10px}
.r-minimal{background:#0a2a1a;color:#4ade80}
.r-limited{background:#2a2000;color:#fbbf24}
.r-high{background:#2a0a0a;color:#f87171}
.r-unacceptable{background:#2a0a18;color:#f472b6}
.chat-main{flex:1;display:flex;flex-direction:column;overflow:hidden}
.messages{flex:1;overflow-y:auto;padding:20px 24px;display:flex;flex-direction:column;gap:16px}
.welcome-screen{text-align:center;margin:auto;max-width:420px}
.welcome-screen h2{font-size:20px;font-weight:500;color:#fff;margin-bottom:8px}
.welcome-screen p{font-size:13px;color:#555;line-height:1.7;margin-bottom:20px}
.eu-compliance-badge{display:inline-block;background:#1a2a4e;color:#93c5fd;border:1px solid #1e3a6e;border-radius:6px;padding:3px 10px;font-size:11px;margin-bottom:14px}
.suggestion-grid{display:grid;grid-template-columns:1fr 1fr;gap:8px}
.suggestion{background:#111119;border:1px solid #1e1e2e;border-radius:8px;padding:10px 12px;font-size:12px;color:#888;cursor:pointer;text-align:left}
.suggestion:hover{border-color:#4f46e5;color:#fff;background:#1e1e40}
.msg{display:flex;flex-direction:column;gap:5px;max-width:680px}
.msg.user{align-self:flex-end;align-items:flex-end}
.msg.assistant{align-self:flex-start;align-items:flex-start}
.bubble{padding:10px 14px;border-radius:10px;font-size:13px;line-height:1.7}
.msg.user .bubble{background:#4f46e5;color:#fff;border-bottom-right-radius:3px}
.msg.assistant .bubble{background:#1a1a28;color:#ddd;border-bottom-left-radius:3px;border:1px solid #2a2a3e}
.msg.blocked-msg .bubble{background:#1f0a0a;border:1px solid #450a0a;color:#f87171}
.msg-meta{font-size:10px;color:#444;display:flex;gap:6px;align-items:center;flex-wrap:wrap}
.reasoning-btn{font-size:11px;color:#4f46e5;background:none;border:none;padding:0;cursor:pointer}
.reasoning-content{background:#0a0a14;border:1px solid #1e1e2e;border-radius:6px;padding:8px 12px;font-size:11px;color:#666;line-height:1.6;margin-top:3px;display:none;max-width:600px}
.reasoning-content.open{display:block}
.typing{display:flex;gap:4px;align-items:center;padding:10px 14px;background:#1a1a28;border-radius:10px;border:1px solid #2a2a3e;border-bottom-left-radius:3px}
.dot{width:6px;height:6px;border-radius:50%;background:#4f46e5;animation:bounce 1.2s infinite}
.dot:nth-child(2){animation-delay:.2s}.dot:nth-child(3){animation-delay:.4s}
@keyframes bounce{0%,60%,100%{transform:translateY(0)}30%{transform:translateY(-5px)}}
.review-waiting{display:flex;gap:8px;align-items:center;padding:10px 14px;background:#1a1500;border-radius:10px;border:1px solid #4a3800;border-bottom-left-radius:3px;font-size:12px;color:#fbbf24}
.spinner{width:14px;height:14px;border:2px solid #4a3800;border-top-color:#fbbf24;border-radius:50%;animation:spin 1s linear infinite;flex-shrink:0}
@keyframes spin{to{transform:rotate(360deg)}}
.input-area{padding:12px 24px 18px;border-top:1px solid #1e1e2e;background:#0a0a12}
.input-row{display:flex;gap:8px;align-items:flex-end;background:#1a1a28;border:1px solid #2a2a3e;border-radius:12px;padding:8px 12px}
.input-row:focus-within{border-color:#4f46e5}
textarea{flex:1;background:transparent;border:none;outline:none;color:#e0e0e0;font-size:13px;resize:none;min-height:22px;max-height:100px;font-family:inherit;line-height:1.5}
textarea::placeholder{color:#3a3a4e}
.send-btn{width:32px;height:32px;background:#4f46e5;border:none;border-radius:7px;cursor:pointer;display:flex;align-items:center;justify-content:center;flex-shrink:0}
.send-btn:hover{background:#4338ca}.send-btn:disabled{background:#2a2a3e;cursor:not-allowed}
.send-btn svg{width:14px;height:14px;fill:white}
.input-hint{font-size:10px;color:#333;text-align:center;margin-top:6px}

/* ── Panel pages ── */
.panel-page{padding:24px}
.panel-header{margin-bottom:24px}
.panel-title{font-size:18px;font-weight:500;color:#fff;margin-bottom:4px}
.panel-sub{font-size:13px;color:#555;line-height:1.6}
.article-tag{display:inline-block;background:#1a2a4e;color:#93c5fd;border-radius:4px;padding:1px 7px;font-size:11px;margin-left:6px}
.stats-grid{display:grid;grid-template-columns:repeat(6,1fr);gap:10px;margin-bottom:20px}
.stat-card{background:#111119;border:1px solid #1e1e2e;border-radius:10px;padding:14px;text-align:center}
.stat-val{font-size:24px;font-weight:600}
.stat-lbl{font-size:10px;color:#555;margin-top:2px;text-transform:uppercase;letter-spacing:0.05em}
.stat-card.s-total .stat-val{color:#e0e0e0}
.stat-card.s-allowed .stat-val{color:#4ade80}
.stat-card.s-blocked .stat-val{color:#f87171}
.stat-card.s-error .stat-val{color:#fbbf24}
.stat-card.s-high .stat-val{color:#f87171}
.stat-card.s-unacceptable .stat-val{color:#f472b6}
.chart-card{background:#111119;border:1px solid #1e1e2e;border-radius:10px;padding:16px;margin-bottom:20px}
.chart-title{font-size:12px;color:#555;margin-bottom:12px;text-transform:uppercase;letter-spacing:0.06em}
.bar-chart{display:flex;align-items:flex-end;gap:4px;height:60px}
.bar-col{display:flex;flex-direction:column;align-items:center;flex:1;gap:3px}
.bar-fill{width:100%;background:#4f46e5;border-radius:2px 2px 0 0;min-height:2px}
.bar-lbl{font-size:9px;color:#444}
.toolbar-row{display:flex;justify-content:space-between;align-items:center;margin-bottom:12px;gap:10px;flex-wrap:wrap}
.toolbar-title{font-size:12px;font-weight:500;color:#888;text-transform:uppercase;letter-spacing:0.06em}
.search-row{display:flex;gap:6px}
.search-row input,.search-row select{background:#111119;border:1px solid #2a2a3e;border-radius:6px;padding:6px 10px;font-size:12px;color:#ccc;outline:none}
.search-row input:focus,.search-row select:focus{border-color:#4f46e5}
.search-row button{padding:6px 12px;background:#4f46e5;color:#fff;border:none;border-radius:6px;font-size:12px;cursor:pointer}
.clear-link{font-size:12px;color:#555;cursor:pointer;padding:6px 8px;text-decoration:none}
.table-wrap{background:#111119;border:1px solid #1e1e2e;border-radius:10px;overflow:hidden;overflow-x:auto}
table{width:100%;border-collapse:collapse;font-size:11px;min-width:800px}
thead{background:#0d0d18}
th{padding:8px 10px;text-align:left;font-weight:500;color:#555;font-size:10px;text-transform:uppercase;letter-spacing:0.04em;border-bottom:1px solid #1e1e2e}
td{padding:8px 10px;border-bottom:1px solid #0d0d18;vertical-align:top;max-width:160px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:#aaa}
tr:last-child td{border-bottom:none}
tr:hover td{background:#0f0f1a}
.b{display:inline-block;padding:2px 7px;border-radius:12px;font-size:10px;font-weight:500}
.b.allowed{background:#14532d;color:#4ade80}
.b.blocked{background:#450a0a;color:#f87171}
.b.error{background:#3a2a00;color:#fbbf24}
.b.review_pending{background:#2a1a00;color:#fb923c}
.rb{display:inline-block;padding:2px 7px;border-radius:12px;font-size:10px;font-weight:500}
.rb.minimal{background:#0a2a1a;color:#4ade80}
.rb.limited{background:#2a2000;color:#fbbf24}
.rb.high{background:#2a0a0a;color:#f87171}
.rb.unacceptable{background:#2a0a18;color:#f472b6}
.detail-a{color:#4f46e5;text-decoration:none;font-size:10px}
.detail-a:hover{text-decoration:underline}
.empty-state{text-align:center;padding:40px;color:#444;font-size:13px}

/* ── Review queue ── */
.review-item{background:#111119;border:1px solid #2a2a3e;border-radius:10px;padding:16px;margin-bottom:10px}
.review-item.urgent{border-left:3px solid #f87171}
.ri-header{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:10px;gap:10px}
.ri-meta{display:flex;gap:6px;align-items:center;flex-wrap:wrap}
.ri-id{font-size:10px;color:#444}
.cat-badge{font-size:10px;padding:2px 8px;border-radius:12px;font-weight:500}
.cat-data_extraction{background:#1a2a4e;color:#93c5fd}
.cat-identity_manipulation{background:#2a1a4e;color:#c4b5fd}
.cat-prompt_injection{background:#2a1a1a;color:#fca5a5}
.cat-jailbreak{background:#2a0a0a;color:#f87171}
.cat-harmful_content{background:#3a0a0a;color:#f87171}
.score-b{font-size:10px;padding:2px 8px;border-radius:12px}
.score-h{background:#3a1010;color:#f87171}.score-m{background:#3a3010;color:#fbbf24}.score-l{background:#0a2a1a;color:#4ade80}
.expires-b{font-size:10px;color:#f87171}
.ri-time{font-size:10px;color:#444}
.prompt-box{background:#0a0a12;border:1px solid #1e1e2e;border-radius:6px;padding:10px 12px;font-size:12px;color:#bbb;line-height:1.6;margin-bottom:8px;white-space:pre-wrap;word-break:break-word}
.reasoning-btn2{font-size:11px;color:#4f46e5;background:none;border:none;padding:0;cursor:pointer;margin-bottom:8px}
.reasoning-box2{background:#080810;border:1px solid #1e1e2e;border-radius:6px;padding:8px 12px;font-size:11px;color:#555;line-height:1.5;margin-bottom:10px;display:none}
.reasoning-box2.open{display:block}
.action-row{display:flex;gap:8px}
.btn-approve{padding:7px 18px;background:#16a34a;color:#fff;border:none;border-radius:7px;font-size:12px;font-weight:500;cursor:pointer}
.btn-approve:hover{background:#15803d}.btn-approve:disabled{background:#0a2a1a;color:#444;cursor:not-allowed}
.btn-reject{padding:7px 18px;background:#dc2626;color:#fff;border:none;border-radius:7px;font-size:12px;font-weight:500;cursor:pointer}
.btn-reject:hover{background:#b91c1c}.btn-reject:disabled{background:#2a0a0a;color:#444;cursor:not-allowed}
.decided-b{font-size:11px;padding:4px 12px;border-radius:7px;font-weight:500}
.decided-approved{background:#14532d;color:#4ade80}
.decided-rejected{background:#450a0a;color:#f87171}
.decided-expired{background:#1e1e28;color:#555}

/* ── Incidents ── */
.incident-card{background:#111119;border:1px solid #2a2a3e;border-radius:10px;padding:16px;margin-bottom:8px}
.incident-card.critical{border-left:3px solid #f472b6}
.incident-card.high{border-left:3px solid #f87171}
.incident-card.medium{border-left:3px solid #fbbf24}
.incident-card.low{border-left:3px solid #4ade80}
.incident-card.resolved{opacity:0.5}
.inc-header{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:8px;gap:10px}
.sev-b{font-size:10px;padding:2px 8px;border-radius:12px;font-weight:500}
.sev-critical{background:#4a0a2e;color:#f472b6}
.sev-high{background:#450a0a;color:#f87171}
.sev-medium{background:#3a2a00;color:#fbbf24}
.sev-low{background:#0a2a1a;color:#4ade80}
.email-b{background:#0a2a1a;color:#4ade80;font-size:10px;padding:2px 7px;border-radius:12px}
.resolved-b{background:#1e2a1e;color:#4ade80;font-size:10px;padding:2px 7px;border-radius:12px}
.inc-prompt{background:#0a0a12;border:1px solid #1e1e2e;border-radius:6px;padding:8px 12px;font-size:12px;color:#aaa;line-height:1.5;margin-bottom:6px;white-space:pre-wrap;word-break:break-word}
.inc-reason{font-size:11px;color:#555;margin-bottom:8px}
.inc-footer{display:flex;justify-content:space-between;align-items:center}
.inc-ip{font-size:10px;color:#444;font-family:monospace}
.resolve-btn{padding:5px 12px;background:#1e3a2e;border:1px solid #16a34a;border-radius:6px;color:#4ade80;font-size:11px;cursor:pointer}
.resolve-btn:hover{background:#14532d}.resolve-btn:disabled{opacity:0.4;cursor:not-allowed}
.filter-row{display:flex;gap:6px;flex-wrap:wrap;margin-bottom:14px}
.filter-btn{padding:5px 12px;background:#111119;border:1px solid #2a2a3e;border-radius:14px;font-size:11px;color:#666;cursor:pointer}
.filter-btn.active{border-color:#4f46e5;color:#fff;background:#1e1e40}
.inc-stats{display:grid;grid-template-columns:repeat(6,1fr);gap:10px;margin-bottom:16px}

/* ── Retention ── */
.retention-card{background:#111119;border:1px solid #2a2a3e;border-radius:10px;padding:20px;margin-bottom:14px}
.rc-title{font-size:14px;font-weight:500;color:#fff;margin-bottom:4px}
.rc-sub{font-size:12px;color:#555;line-height:1.5;margin-bottom:16px}
.ret-stats{display:grid;grid-template-columns:repeat(3,1fr);gap:10px;margin-bottom:16px}
.form-grp{margin-bottom:12px}
.form-lbl{font-size:11px;color:#666;margin-bottom:5px;display:block}
.form-inp{width:100%;background:#0a0a12;border:1px solid #2a2a3e;border-radius:6px;padding:8px 12px;font-size:13px;color:#e0e0e0;outline:none}
.form-inp:focus{border-color:#4f46e5}
.form-row{display:flex;gap:8px;align-items:flex-end}
.btn-primary{padding:8px 16px;background:#4f46e5;color:#fff;border:none;border-radius:7px;font-size:12px;font-weight:500;cursor:pointer}
.btn-primary:hover{background:#4338ca}.btn-primary:disabled{background:#2a2a3e;cursor:not-allowed}
.btn-warn{padding:8px 16px;background:#7c2d12;color:#fff;border:none;border-radius:7px;font-size:12px;cursor:pointer}
.btn-warn:hover{background:#9a3412}
.btn-danger{padding:8px 16px;background:#450a0a;color:#f87171;border:1px solid #7f1d1d;border-radius:7px;font-size:12px;cursor:pointer}
.btn-danger:hover{background:#7f1d1d;color:#fff}
.warn-box{background:#1f1000;border:1px solid #4a3000;border-radius:6px;padding:10px 12px;font-size:11px;color:#fbbf24;line-height:1.5;margin-bottom:12px}
.result-box{background:#0a0a12;border:1px solid #2a2a3e;border-radius:6px;padding:10px 12px;margin-top:10px;font-size:12px;color:#4ade80;line-height:1.5;display:none}
.result-box.error{color:#f87171}
.result-box.show{display:block}
.info-row{display:flex;justify-content:space-between;padding:8px 0;border-bottom:1px solid #1e1e2e;font-size:12px}
.info-row:last-child{border-bottom:none}
.info-lbl{color:#555}
.info-val{color:#aaa;font-family:monospace}
</style>
</head>
<body>

<!-- Sidebar -->
<div class="sidebar">
  <div class="sidebar-logo">
    <h1>AI Gateway</h1>
    <p>Governance Platform</p>
    <span class="eu-pill">EU AI Act Compliant</span>
  </div>
  <nav class="nav">
    <div class="nav-section">Main</div>
    <button class="nav-item active" onclick="showPage('chat')" id="nav-chat">
      <i class="ti ti-message nav-icon" aria-hidden="true"></i> Chat
    </button>

    <div class="nav-section">Governance</div>
    <button class="nav-item" onclick="showPage('audit')" id="nav-audit">
      <i class="ti ti-file-text nav-icon" aria-hidden="true"></i> Audit Log
    </button>
    <button class="nav-item" onclick="showPage('review')" id="nav-review">
      <i class="ti ti-user-check nav-icon" aria-hidden="true"></i> Review Queue
      <span class="nav-badge warn" id="review-count" style="display:none">0</span>
    </button>
    <button class="nav-item" onclick="showPage('incidents')" id="nav-incidents">
      <i class="ti ti-alert-triangle nav-icon" aria-hidden="true"></i> Incidents
      <span class="nav-badge" id="incident-count" style="display:none">0</span>
    </button>

    <div class="nav-section">Compliance</div>
    <button class="nav-item" onclick="showPage('retention')" id="nav-retention">
      <i class="ti ti-database nav-icon" aria-hidden="true"></i> Retention &amp; GDPR
    </button>
  </nav>
  <div class="sidebar-footer">
    <span class="status-dot"></span>Gateway running
  </div>
</div>

<!-- Main -->
<div class="main">
  <div class="topbar">
    <span class="topbar-title" id="page-title">Chat</span>
    <div class="topbar-right">
      <input class="key-input" id="apiKey" type="password" placeholder="X-API-Key..." />
    </div>
  </div>
  <div class="content">

    <!-- ── CHAT PAGE ── -->
    <div class="page active" id="page-chat" style="flex-direction:column">
      <div class="chat-layout">
        <div class="chat-history">
          <div class="chat-history-title">History</div>
          <div id="chatHistory"></div>
        </div>
        <div class="chat-main">
          <div class="messages" id="messages">
            <div class="welcome-screen" id="welcome">
              <span class="eu-compliance-badge">EU AI Act — Articles 5, 9, 13, 14, 52</span>
              <h2>AI Gateway Platform</h2>
              <p>Every request is authenticated, classified with full reasoning chain, risk-scored against the EU AI Act, and logged with complete auditability.</p>
              <div class="suggestion-grid">
                <button class="suggestion" onclick="useSug(this)">What is machine learning?</button>
                <button class="suggestion" onclick="useSug(this)">Explain Docker in simple terms</button>
                <button class="suggestion" onclick="useSug(this)">What is a REST API?</button>
                <button class="suggestion" onclick="useSug(this)">How does rate limiting work?</button>
              </div>
            </div>
          </div>
          <div class="input-area">
            <div class="input-row">
              <textarea id="prompt" placeholder="Type a message and press Enter..." rows="1"></textarea>
              <button class="send-btn" id="sendBtn" onclick="sendMsg()" title="Send">
                <svg viewBox="0 0 24 24"><path d="M2 21L23 12 2 3v7l15 2-15 2z"/></svg>
              </button>
            </div>
            <div class="input-hint">Enter to send · Shift+Enter for new line</div>
          </div>
        </div>
      </div>
    </div>

    <!-- ── AUDIT LOG PAGE ── -->
    <div class="page" id="page-audit">
      <div class="panel-page">
        <div class="panel-header">
          <p class="panel-title">Audit Log</p>
          <p class="panel-sub">Complete record of every request with reasoning chain, risk level, and EU AI Act article citations.</p>
        </div>
        <div class="stats-grid" id="auditStats">
          <div class="stat-card s-total"><div class="stat-val" id="as-total">—</div><div class="stat-lbl">Total</div></div>
          <div class="stat-card s-allowed"><div class="stat-val" id="as-allowed">—</div><div class="stat-lbl">Allowed</div></div>
          <div class="stat-card s-blocked"><div class="stat-val" id="as-blocked">—</div><div class="stat-lbl">Blocked</div></div>
          <div class="stat-card s-error"><div class="stat-val" id="as-errors">—</div><div class="stat-lbl">Errors</div></div>
          <div class="stat-card s-high"><div class="stat-val" id="as-high">—</div><div class="stat-lbl">High risk</div></div>
          <div class="stat-card s-unacceptable"><div class="stat-val" id="as-unacceptable">—</div><div class="stat-lbl">Unacceptable</div></div>
        </div>
        <div class="chart-card">
          <div class="chart-title">Requests by hour (last 24h)</div>
          <div class="bar-chart" id="auditChart"><div class="empty-state" style="padding:10px;color:#444;font-size:11px">No data yet</div></div>
        </div>
        <div class="toolbar-row">
          <span class="toolbar-title">Recent requests</span>
          <div class="search-row">
            <input id="auditSearch" type="text" placeholder="Search..." onkeydown="if(event.key==='Enter')loadAudit()"/>
            <select id="auditRisk" onchange="loadAudit()">
              <option value="">All risks</option>
              <option value="minimal">Minimal</option>
              <option value="limited">Limited</option>
              <option value="high">High</option>
              <option value="unacceptable">Unacceptable</option>
            </select>
            <button onclick="loadAudit()">Filter</button>
            <a class="clear-link" onclick="clearAudit()">Clear</a>
          </div>
        </div>
        <div class="table-wrap">
          <table>
            <thead><tr>
              <th>#</th><th>Time</th><th>Prompt</th><th>Status</th>
              <th>Risk</th><th>Category</th><th>Score</th><th>Article</th><th>Detail</th>
            </tr></thead>
            <tbody id="auditBody"><tr><td colspan="9" class="empty-state">Loading...</td></tr></tbody>
          </table>
        </div>
      </div>
    </div>

    <!-- ── REVIEW QUEUE PAGE ── -->
    <div class="page" id="page-review">
      <div class="panel-page">
        <div class="panel-header">
          <p class="panel-title">Human Review Queue <span class="article-tag">Article 14</span></p>
          <p class="panel-sub">Borderline and sensitive-category requests held for human judgment. Approve to forward to Groq, reject to block. Items expire in 5 minutes and block by default.</p>
        </div>
        <div class="stats-grid" style="grid-template-columns:repeat(4,1fr)">
          <div class="stat-card s-error"><div class="stat-val" id="rv-pending">—</div><div class="stat-lbl">Pending</div></div>
          <div class="stat-card s-allowed"><div class="stat-val" id="rv-approved">—</div><div class="stat-lbl">Approved</div></div>
          <div class="stat-card s-blocked"><div class="stat-val" id="rv-rejected">—</div><div class="stat-lbl">Rejected</div></div>
          <div class="stat-card s-total"><div class="stat-val" id="rv-expired">—</div><div class="stat-lbl">Expired</div></div>
        </div>
        <div class="toolbar-title" style="margin-bottom:10px">Pending — awaiting decision</div>
        <div id="reviewPending"></div>
        <div class="toolbar-title" style="margin:20px 0 10px">Recent decisions</div>
        <div id="reviewHistory"></div>
      </div>
    </div>

    <!-- ── INCIDENTS PAGE ── -->
    <div class="page" id="page-incidents">
      <div class="panel-page">
        <div class="panel-header">
          <p class="panel-title">Security Incidents</p>
          <p class="panel-sub">Every high-confidence block, Article 5 violation, and human rejection creates an incident. Critical and high severity trigger email alerts via Resend.</p>
        </div>
        <div class="inc-stats">
          <div class="stat-card s-total"><div class="stat-val" id="inc-total">—</div><div class="stat-lbl">Total</div></div>
          <div class="stat-card s-blocked"><div class="stat-val" id="inc-unresolved">—</div><div class="stat-lbl">Unresolved</div></div>
          <div class="stat-card s-unacceptable"><div class="stat-val" id="inc-critical">—</div><div class="stat-lbl">Critical</div></div>
          <div class="stat-card s-blocked"><div class="stat-val" id="inc-high">—</div><div class="stat-lbl">High</div></div>
          <div class="stat-card s-error"><div class="stat-val" id="inc-medium">—</div><div class="stat-lbl">Medium</div></div>
          <div class="stat-card s-allowed"><div class="stat-val" id="inc-low">—</div><div class="stat-lbl">Low</div></div>
        </div>
        <div class="filter-row" id="incFilter">
          <button class="filter-btn active" onclick="setIncFilter('',this)">All</button>
          <button class="filter-btn" onclick="setIncFilter('critical',this)">Critical</button>
          <button class="filter-btn" onclick="setIncFilter('high',this)">High</button>
          <button class="filter-btn" onclick="setIncFilter('medium',this)">Medium</button>
          <button class="filter-btn" onclick="setIncFilter('low',this)">Low</button>
        </div>
        <div id="incidentsList"></div>
      </div>
    </div>

    <!-- ── RETENTION PAGE ── -->
    <div class="page" id="page-retention">
      <div class="panel-page">
        <div class="panel-header">
          <p class="panel-title">Data Retention &amp; GDPR Erasure <span class="article-tag">Article 17</span></p>
          <p class="panel-sub">Configure log retention, trigger manual purges, and exercise GDPR Article 17 right to erasure for specific API keys.</p>
        </div>

        <div class="retention-card" id="retentionStatus">
          <p class="rc-title">Current policy</p>
          <p class="rc-sub">Loading...</p>
        </div>

        <div class="retention-card">
          <p class="rc-title">Update retention period</p>
          <p class="rc-sub">Records older than this many days are automatically deleted during the nightly purge. Unresolved incidents are always kept.</p>
          <div class="form-row">
            <div class="form-grp" style="flex:1">
              <label class="form-lbl">Retention days</label>
              <input class="form-inp" type="number" id="retDays" min="1" max="3650" value="90"/>
            </div>
            <div class="form-grp" style="flex:0.6">
              <label class="form-lbl">Updated by</label>
              <input class="form-inp" type="text" id="retUpdatedBy" value="admin"/>
            </div>
            <button class="btn-primary" onclick="updateRetention()">Update</button>
          </div>
          <div class="result-box" id="retResult"></div>
        </div>

        <div class="retention-card">
          <p class="rc-title">Manual purge</p>
          <p class="rc-sub">Immediately purge all records older than the current retention period. Same as the nightly scheduled purge.</p>
          <button class="btn-warn" onclick="triggerPurge()">Run purge now</button>
          <div class="result-box" id="purgeResult"></div>
        </div>

        <div class="retention-card">
          <p class="rc-title">GDPR right to erasure <span class="article-tag" style="background:#2a1a4e;color:#c4b5fd">GDPR Art. 17</span></p>
          <p class="rc-sub">Permanently delete all audit log entries for a specific API key. This action cannot be undone.</p>
          <div class="warn-box">⚠ Permanent deletion — all audit logs for the specified key will be removed and cannot be recovered.</div>
          <div class="form-row">
            <div class="form-grp" style="flex:1">
              <label class="form-lbl">API key to erase</label>
              <input class="form-inp" type="text" id="eraseKey" placeholder="key-alpha-123" style="font-family:monospace"/>
            </div>
            <button class="btn-danger" onclick="eraseData()" id="eraseBtn">Erase all data</button>
          </div>
          <div class="result-box" id="eraseResult"></div>
        </div>
      </div>
    </div>

  </div>
</div>

<script>
// ── State ──────────────────────────────────────────────────────────────────
const chatHistory = [];
let incFilter = '';
let deciding = {};
let resolving = {};
let pollIntervals = {};

// ── Navigation ─────────────────────────────────────────────────────────────
const pageTitles = {
  chat: 'Chat', audit: 'Audit Log',
  review: 'Review Queue', incidents: 'Incidents', retention: 'Retention & GDPR'
};

function showPage(id) {
  document.querySelectorAll('.page').forEach(p => p.classList.remove('active'));
  document.querySelectorAll('.nav-item').forEach(n => n.classList.remove('active'));
  document.getElementById('page-' + id).classList.add('active');
  document.getElementById('nav-' + id).classList.add('active');
  document.getElementById('page-title').textContent = pageTitles[id];

  if (id === 'audit')     loadAudit();
  if (id === 'review')    loadReview();
  if (id === 'incidents') loadIncidents();
  if (id === 'retention') loadRetention();

  // Stop polling other pages
  Object.keys(pollIntervals).forEach(k => { if (k !== id) { clearInterval(pollIntervals[k]); delete pollIntervals[k]; }});

  if (id === 'review' && !pollIntervals.review) {
    pollIntervals.review = setInterval(loadReview, 3000);
  }
  if (id === 'incidents' && !pollIntervals.incidents) {
    pollIntervals.incidents = setInterval(loadIncidents, 5000);
  }
}

// Check URL hash on load
(function() {
  const hash = location.hash.replace('#','');
  if (['audit','review','incidents','retention'].includes(hash)) showPage(hash);
})();

// ── Helpers ────────────────────────────────────────────────────────────────
function esc(s) { return (s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }
function trunc(s, n) { return s && s.length > n ? s.substring(0, n) + '…' : (s||''); }
function scoreClass(v) { return v >= 0.7 ? 'score-h' : v >= 0.4 ? 'score-m' : 'score-l'; }
function timeUntil(exp) {
  const diff = Math.max(0, Math.floor((new Date(exp.replace(' ','T')+'Z') - Date.now()) / 1000));
  if (!diff) return 'expired';
  return diff >= 60 ? Math.floor(diff/60)+'m '+diff%60+'s' : diff+'s';
}

// ── Periodic badge refresh ─────────────────────────────────────────────────
async function refreshBadges() {
  try {
    const [rv, inc] = await Promise.all([
      fetch('/admin/review/stats').then(r=>r.json()),
      fetch('/admin/incidents/stats').then(r=>r.json())
    ]);
    const rp = rv.stats?.pending || 0;
    const el = document.getElementById('review-count');
    el.textContent = rp; el.style.display = rp ? '' : 'none';
    const iu = inc.stats?.unresolved || 0;
    const el2 = document.getElementById('incident-count');
    el2.textContent = iu; el2.style.display = iu ? '' : 'none';
  } catch(e){}
}
refreshBadges(); setInterval(refreshBadges, 10000);

// ── CHAT ───────────────────────────────────────────────────────────────────
const ta = document.getElementById('prompt');
ta.addEventListener('input', () => { ta.style.height='auto'; ta.style.height=Math.min(ta.scrollHeight,100)+'px'; });
ta.addEventListener('keydown', e => { if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();sendMsg();} });
function useSug(btn) { ta.value = btn.textContent; ta.focus(); }
function getKey() { return document.getElementById('apiKey').value.trim(); }

function removeWelcome() { const w=document.getElementById('welcome'); if(w) w.remove(); }

function addMsg(role, text, meta) {
  removeWelcome();
  const msgs = document.getElementById('messages');
  const div = document.createElement('div');
  let cls = 'msg '+role;
  if (meta?.status==='blocked') cls += ' blocked-msg';
  div.className = cls;

  const bubble = document.createElement('div');
  bubble.className = 'bubble';
  bubble.textContent = text;
  div.appendChild(bubble);

  if (meta) {
    const m = document.createElement('div');
    m.className = 'msg-meta';
    m.innerHTML = new Date().toLocaleTimeString();
    if (meta.status) m.innerHTML += ' · <span class="b '+meta.status+'">'+meta.status+'</span>';
    if (meta.risk_level) m.innerHTML += ' · <span class="r-sm r-'+meta.risk_level+'">'+meta.risk_level+' risk</span>';
    if (meta.eu_article) m.innerHTML += ' · '+esc(meta.eu_article);
    div.appendChild(m);

    if (meta.reasoning_chain) {
      const btn = document.createElement('button');
      btn.className = 'reasoning-btn';
      btn.textContent = 'View reasoning ↓';
      const box = document.createElement('div');
      box.className = 'reasoning-content';
      box.textContent = meta.reasoning_chain;
      btn.onclick = () => { box.classList.toggle('open'); btn.textContent = box.classList.contains('open') ? 'Hide reasoning ↑' : 'View reasoning ↓'; };
      div.appendChild(btn); div.appendChild(box);
    }
  }
  msgs.appendChild(div);
  msgs.scrollTop = msgs.scrollHeight;
}

function addTyping(review) {
  removeWelcome();
  const msgs = document.getElementById('messages');
  const div = document.createElement('div');
  div.className = 'msg assistant'; div.id = 'typing-indicator';
  if (review) {
    div.innerHTML = '<div class="review-waiting"><div class="spinner"></div>Waiting for human review — <span style="cursor:pointer;color:#fbbf24;font-weight:500;text-decoration:underline" onclick="showPage(\'review\')">open review queue</span> to decide</div>';
  } else {
    div.innerHTML = '<div class="typing"><div class="dot"></div><div class="dot"></div><div class="dot"></div></div>';
  }
  msgs.appendChild(div); msgs.scrollTop = msgs.scrollHeight;
}

function removeTyping() { const t=document.getElementById('typing-indicator'); if(t)t.remove(); }

function addToHistory(prompt, status, risk) {
  chatHistory.unshift({prompt,status,risk});
  const list = document.getElementById('chatHistory');
  list.innerHTML = '';
  chatHistory.slice(0,20).forEach((item,i)=>{
    const div = document.createElement('div');
    div.className = 'history-item'+(i===0?' active':'');
    div.innerHTML = '<div class="h-prompt">'+esc(item.prompt)+'</div>' +
      '<div class="h-meta"><span class="b-sm '+item.status+'">'+item.status+'</span>' +
      (item.risk?'<span class="r-sm r-'+item.risk+'">'+item.risk+'</span>':'')+'</div>';
    div.onclick = () => { ta.value=item.prompt; ta.focus(); };
    list.appendChild(div);
  });
}

async function sendMsg() {
  const prompt = ta.value.trim();
  if (!prompt) return;
  const key = getKey();
  if (!key) { alert('Enter your API key in the top bar.\n\nValid keys: key-alpha-123, key-beta-456, key-gamma-789'); return; }

  ta.value=''; ta.style.height='auto';
  document.getElementById('sendBtn').disabled = true;
  addMsg('user', prompt, null);

  const likely = ['repeat','instructions','system prompt','told','pretend','inject'].some(w=>prompt.toLowerCase().includes(w));
  addTyping(likely);

  try {
    const res = await fetch('/ai', {
      method:'POST',
      headers:{'Content-Type':'application/json','X-API-Key':key},
      body: JSON.stringify({prompt})
    });
    const data = await res.json();
    removeTyping();

    if (res.status===401) {
      addMsg('assistant','Invalid or missing API key.',{status:'error'});
      addToHistory(prompt,'error',null);
    } else if (res.status===451) {
      addMsg('assistant','Blocked — EU AI Act Article 5 violation\n\n'+data.reason,{status:'blocked',risk_level:'unacceptable',eu_article:data.article});
      addToHistory(prompt,'blocked','unacceptable');
    } else if (res.status===403) {
      addMsg('assistant','Blocked: '+data.reason,{status:'blocked',risk_level:data.risk_level,eu_article:data.eu_article,reasoning_chain:data.reasoning_chain});
      addToHistory(prompt,'blocked',data.risk_level);
    } else if (res.ok) {
      addMsg('assistant',data.response,{status:'allowed',risk_level:data.risk_level});
      addToHistory(prompt,'allowed',data.risk_level);
    } else {
      addMsg('assistant','Error: '+(data.reason||'something went wrong'),{status:'error'});
      addToHistory(prompt,'error',null);
    }
  } catch(err) {
    removeTyping();
    addMsg('assistant','Could not reach the gateway.',{status:'error'});
    addToHistory(prompt,'error',null);
  }
  document.getElementById('sendBtn').disabled=false;
  ta.focus();
}

// ── AUDIT LOG ──────────────────────────────────────────────────────────────
async function loadAudit() {
  const search = document.getElementById('auditSearch').value;
  const risk = document.getElementById('auditRisk').value;
  const params = new URLSearchParams();
  if (search) params.set('search', search);
  if (risk) params.set('risk', risk);

  try {
    const res = await fetch('/dashboard?'+params.toString());
    const data = await res.json();
    const s = data.stats||{};
    document.getElementById('as-total').textContent = s.Total||0;
    document.getElementById('as-allowed').textContent = s.Allowed||0;
    document.getElementById('as-blocked').textContent = s.Blocked||0;
    document.getElementById('as-errors').textContent = s.Errors||0;
    document.getElementById('as-high').textContent = s.HighRisk||0;
    document.getElementById('as-unacceptable').textContent = s.Unacceptable||0;

    // Chart
    const chart = document.getElementById('auditChart');
    if (data.buckets && data.buckets.length) {
      const max = Math.max(...data.buckets.map(b=>b.Count));
      chart.innerHTML = data.buckets.map(b=>{
        const h = Math.max(2, Math.round((b.Count/max)*56));
        return '<div class="bar-col"><div class="bar-fill" style="height:'+h+'px" title="'+b.Count+'"></div><div class="bar-lbl">'+esc(b.Hour)+'</div></div>';
      }).join('');
    } else {
      chart.innerHTML = '<div style="color:#444;font-size:11px;text-align:center;padding:10px;width:100%">No data yet</div>';
    }

    // Table
    const body = document.getElementById('auditBody');
    if (!data.logs || !data.logs.length) {
      body.innerHTML = '<tr><td colspan="9" class="empty-state">No requests yet</td></tr>';
      return;
    }
    body.innerHTML = data.logs.map(l=>'<tr>' +
      '<td>'+l.ID+'</td>' +
      '<td>'+esc(l.Timestamp)+'</td>' +
      '<td title="'+esc(l.Prompt)+'">'+esc(trunc(l.Prompt,40))+'</td>' +
      '<td><span class="b '+l.Status+'">'+l.Status+'</span></td>' +
      '<td><span class="rb '+l.RiskLevel+'">'+l.RiskLevel+'</span></td>' +
      '<td>'+esc(l.Category||'—')+'</td>' +
      '<td>'+l.ClassifierScore.toFixed(2)+'</td>' +
      '<td>'+esc(l.EUArticle||'—')+'</td>' +
      '<td><a class="detail-a" href="/admin/audit/'+l.ID+'" target="_blank">View →</a></td>' +
    '</tr>').join('');
  } catch(e){ console.error('Audit load error',e); }
}

function clearAudit() {
  document.getElementById('auditSearch').value='';
  document.getElementById('auditRisk').value='';
  loadAudit();
}

// ── REVIEW QUEUE ───────────────────────────────────────────────────────────
async function loadReview() {
  try {
    const [p, a, s] = await Promise.all([
      fetch('/admin/review').then(r=>r.json()),
      fetch('/admin/review/all').then(r=>r.json()),
      fetch('/admin/review/stats').then(r=>r.json())
    ]);

    const stats = s.stats||{};
    document.getElementById('rv-pending').textContent = stats.pending||0;
    document.getElementById('rv-approved').textContent = stats.approved||0;
    document.getElementById('rv-rejected').textContent = stats.rejected||0;
    document.getElementById('rv-expired').textContent = stats.expired||0;

    renderPending(p.items||[]);
    renderReviewHistory((a.items||[]).filter(i=>i.status!=='pending'));
  } catch(e){ console.error('Review load error',e); }
}

function renderPending(items) {
  const el = document.getElementById('reviewPending');
  if (!items.length) {
    el.innerHTML = '<div class="empty-state">No pending items — queue is clear ✓</div>';
    return;
  }
  el.innerHTML = items.map(item => {
    const until = timeUntil(item.expires_at);
    const urgent = until!=='expired' && parseInt(until)<60;
    const dis = deciding[item.id] ? 'disabled' : '';
    return '<div class="review-item'+(urgent?' urgent':'')+'" id="ri-'+item.id+'">' +
      '<div class="ri-header">' +
        '<div class="ri-meta">' +
          '<span class="ri-id">#'+item.id+'</span>' +
          '<span class="cat-badge cat-'+esc(item.category)+'">'+esc(item.category||'unknown')+'</span>' +
          '<span class="score-b '+scoreClass(item.score)+'">'+item.score.toFixed(2)+'</span>' +
          '<span class="expires-b">expires '+until+'</span>' +
        '</div>' +
        '<span class="ri-time">'+esc(item.created_at)+'</span>' +
      '</div>' +
      '<div class="prompt-box">'+esc(item.prompt)+'</div>' +
      (item.reasoning?'<button class="reasoning-btn2" onclick="toggleR2(this)">View AI reasoning ↓</button><div class="reasoning-box2">'+esc(item.reasoning)+'</div>':'') +
      '<div class="action-row">' +
        '<button class="btn-approve" onclick="reviewDecide('+item.id+',\'approve\')" '+dis+'>✓ Approve</button>' +
        '<button class="btn-reject" onclick="reviewDecide('+item.id+',\'reject\')" '+dis+'>✗ Reject</button>' +
      '</div>' +
    '</div>';
  }).join('');
}

function renderReviewHistory(items) {
  const el = document.getElementById('reviewHistory');
  if (!items.length) { el.innerHTML='<div class="empty-state" style="padding:20px">No decisions yet</div>'; return; }
  el.innerHTML = items.slice(0,15).map(item=>'<div style="display:flex;justify-content:space-between;align-items:center;padding:8px 10px;background:#0d0d18;border-radius:6px;margin-bottom:4px;gap:10px">' +
    '<span style="font-size:12px;color:#888;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="'+esc(item.prompt)+'">'+esc(trunc(item.prompt,60))+'</span>' +
    '<div style="display:flex;gap:6px;flex-shrink:0">' +
      '<span class="cat-badge cat-'+esc(item.category)+'">'+esc(item.category||'')+'</span>' +
      '<span class="decided-b decided-'+item.status+'">'+item.status+'</span>' +
    '</div>' +
  '</div>').join('');
}

function toggleR2(btn) {
  const box = btn.nextElementSibling;
  box.classList.toggle('open');
  btn.textContent = box.classList.contains('open') ? 'Hide AI reasoning ↑' : 'View AI reasoning ↓';
}

async function reviewDecide(id, action) {
  deciding[id] = true;
  renderPending((await fetch('/admin/review').then(r=>r.json())).items||[]);
  try {
    await fetch('/admin/review/'+action, {
      method:'POST', headers:{'Content-Type':'application/json'},
      body: JSON.stringify({id, reviewer:'admin'})
    });
  } catch(e){ alert('Error: '+e.message); }
  deciding[id]=false;
  loadReview();
}

// ── INCIDENTS ──────────────────────────────────────────────────────────────
function setIncFilter(f, btn) {
  incFilter = f;
  document.querySelectorAll('#incFilter .filter-btn').forEach(b=>b.classList.remove('active'));
  btn.classList.add('active');
  loadIncidents();
}

async function loadIncidents() {
  try {
    const url = '/admin/incidents'+(incFilter?'?severity='+incFilter:'');
    const [incData, statsData] = await Promise.all([
      fetch(url).then(r=>r.json()),
      fetch('/admin/incidents/stats').then(r=>r.json())
    ]);

    const s = statsData.stats||{};
    document.getElementById('inc-total').textContent = s.total||0;
    document.getElementById('inc-unresolved').textContent = s.unresolved||0;
    document.getElementById('inc-critical').textContent = s.critical||0;
    document.getElementById('inc-high').textContent = s.high||0;
    document.getElementById('inc-medium').textContent = s.medium||0;
    document.getElementById('inc-low').textContent = s.low||0;

    renderIncidents(incData.incidents||[]);
  } catch(e){ console.error('Incidents load error',e); }
}

function renderIncidents(items) {
  const el = document.getElementById('incidentsList');
  if (!items.length) { el.innerHTML='<div class="empty-state">No incidents'+(incFilter?' for severity "'+incFilter+'"':'')+'</div>'; return; }
  el.innerHTML = items.map(inc=>{
    const cls = 'incident-card '+inc.severity+(inc.resolved?' resolved':'');
    const prompt = trunc(inc.prompt, 180);
    const dis = resolving[inc.id]?'disabled':'';
    return '<div class="'+cls+'">' +
      '<div class="inc-header">' +
        '<div style="display:flex;gap:6px;align-items:center;flex-wrap:wrap">' +
          '<span style="font-size:10px;color:#444">#'+inc.id+'</span>' +
          '<span class="sev-b sev-'+inc.severity+'">'+inc.severity.toUpperCase()+'</span>' +
          '<span class="cat-badge cat-'+esc(inc.category)+'">'+esc(inc.category)+'</span>' +
          (inc.eu_article?'<span style="background:#1a2a4e;color:#93c5fd;font-size:10px;padding:2px 7px;border-radius:12px">'+esc(inc.eu_article)+'</span>':'') +
          (inc.email_sent?'<span class="email-b">📧 alerted</span>':'') +
          (inc.resolved?'<span class="resolved-b">✓ resolved by '+esc(inc.resolved_by)+'</span>':'') +
        '</div>' +
        '<span style="font-size:10px;color:#444;flex-shrink:0">'+esc(inc.timestamp)+'</span>' +
      '</div>' +
      '<div class="inc-prompt">'+esc(prompt)+'</div>' +
      '<div class="inc-reason">'+esc(inc.reason)+'</div>' +
      '<div class="inc-footer">' +
        '<span class="inc-ip">'+esc(inc.client_ip)+'</span>' +
        (!inc.resolved?'<button class="resolve-btn" onclick="resolveInc('+inc.id+',this)" '+dis+'>Mark resolved</button>':'') +
      '</div>' +
    '</div>';
  }).join('');
}

async function resolveInc(id, btn) {
  resolving[id]=true; btn.disabled=true; btn.textContent='Resolving...';
  try {
    await fetch('/admin/incidents/resolve', {
      method:'POST', headers:{'Content-Type':'application/json'},
      body: JSON.stringify({id, resolved_by:'admin'})
    });
  } catch(e){ alert('Error: '+e.message); }
  resolving[id]=false;
  loadIncidents();
}

// ── RETENTION ──────────────────────────────────────────────────────────────
async function loadRetention() {
  try {
    const res = await fetch('/admin/retention');
    const data = await res.json();
    const p = data.policy||{};
    const s = data.storage||{};
    document.getElementById('retDays').value = p.retention_days||90;
    document.getElementById('retentionStatus').innerHTML =
      '<p class="rc-title">Current policy</p>' +
      '<div class="ret-stats">' +
        '<div class="stat-card s-total"><div class="stat-val">'+( s.audit_logs||0)+'</div><div class="stat-lbl">Audit logs</div></div>' +
        '<div class="stat-card s-total"><div class="stat-val">'+(s.incidents||0)+'</div><div class="stat-lbl">Incidents</div></div>' +
        '<div class="stat-card s-total"><div class="stat-val">'+(s.review_queue||0)+'</div><div class="stat-lbl">Review items</div></div>' +
      '</div>' +
      '<div class="info-row"><span class="info-lbl">Retention period</span><span class="info-val">'+(p.retention_days||'—')+' days</span></div>' +
      '<div class="info-row"><span class="info-lbl">Oldest log</span><span class="info-val">'+(s.oldest_log||'—')+'</span></div>' +
      '<div class="info-row"><span class="info-lbl">Newest log</span><span class="info-val">'+(s.newest_log||'—')+'</span></div>' +
      '<div class="info-row"><span class="info-lbl">Last updated by</span><span class="info-val">'+(p.updated_by||'—')+' on '+(p.updated_at||'—')+'</span></div>';
  } catch(e){ console.error('Retention load error',e); }
}

async function updateRetention() {
  const days = parseInt(document.getElementById('retDays').value);
  const by = document.getElementById('retUpdatedBy').value||'admin';
  const el = document.getElementById('retResult');
  try {
    const res = await fetch('/admin/retention',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({days,updated_by:by})});
    const data = await res.json();
    el.className='result-box show'+(res.ok?'':' error');
    el.textContent = res.ok ? '✓ '+data.message : '✗ '+data.reason;
    if (res.ok) loadRetention();
  } catch(e){ el.className='result-box show error'; el.textContent='✗ '+e.message; }
}

async function triggerPurge() {
  const el = document.getElementById('purgeResult');
  el.className='result-box show'; el.textContent='Running purge...';
  try {
    const res = await fetch('/admin/retention/purge',{method:'POST'});
    const data = await res.json();
    el.className='result-box show'+(res.ok?'':' error');
    if (res.ok) {
      const r=data.result;
      el.textContent='✓ Purge complete — '+r.audit_logs_deleted+' audit logs, '+r.incidents_deleted+' incidents, '+r.review_items_deleted+' review items deleted';
      loadRetention();
    } else { el.textContent='✗ '+data.reason; }
  } catch(e){ el.className='result-box show error'; el.textContent='✗ '+e.message; }
}

async function eraseData() {
  const key = document.getElementById('eraseKey').value.trim();
  const el = document.getElementById('eraseResult');
  const btn = document.getElementById('eraseBtn');
  if (!key) { el.className='result-box show error'; el.textContent='✗ Enter an API key'; return; }
  if (!confirm('Permanently delete all audit logs for "'+key+'"? This cannot be undone.')) return;
  btn.disabled=true; btn.textContent='Erasing...';
  el.className='result-box show'; el.textContent='Processing...';
  try {
    const res = await fetch('/admin/retention/erase',{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({api_key:key})});
    const data = await res.json();
    el.className='result-box show'+(res.ok?'':' error');
    el.textContent = res.ok ? '✓ '+data.message : '✗ '+data.reason;
    if (res.ok) loadRetention();
  } catch(e){ el.className='result-box show error'; el.textContent='✗ '+e.message; }
  btn.disabled=false; btn.textContent='Erase all data';
}
</script>
</body>
</html>`
