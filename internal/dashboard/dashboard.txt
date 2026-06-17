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

func (d *Dashboard) HandleHome(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.New("home").Parse(homeHTML)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	tmpl.Execute(w, nil)
}

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

	data := struct {
		Logs       []LogEntry
		Stats      Stats
		Buckets    []HourBucket
		Timestamp  string
		Search     string
		RiskFilter string
	}{
		Logs:       logs,
		Stats:      stats,
		Buckets:    buckets,
		Timestamp:  time.Now().Format("2006-01-02 15:04:05"),
		Search:     search,
		RiskFilter: riskFilter,
	}

	tmpl, err := template.New("dashboard").Parse(dashboardHTML)
	if err != nil {
		http.Error(w, "Template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	tmpl.Execute(w, data)
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

func (d *Dashboard) HandleNotFound(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "404 — not found")
}

const homeHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AI Gateway</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #0f0f1a; color: #e0e0e0; height: 100vh; display: flex; flex-direction: column; }
  .topbar { background: #1a1a2e; padding: 14px 28px; display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid #2a2a3e; }
  .topbar h1 { font-size: 16px; font-weight: 500; color: #fff; }
  .topbar-right { display: flex; gap: 12px; align-items: center; }
  .nav-link { font-size: 13px; color: #888; text-decoration: none; padding: 6px 12px; border-radius: 6px; }
  .nav-link:hover { background: #2a2a3e; color: #fff; }
  .key-input { background: #2a2a3e; border: 1px solid #3a3a5e; border-radius: 6px; padding: 6px 12px; font-size: 12px; color: #ccc; width: 200px; outline: none; font-family: monospace; }
  .key-input:focus { border-color: #6366f1; }
  .main { flex: 1; display: flex; overflow: hidden; }
  .sidebar { width: 260px; background: #13131f; border-right: 1px solid #2a2a3e; display: flex; flex-direction: column; padding: 16px; gap: 8px; overflow-y: auto; }
  .sidebar-title { font-size: 11px; color: #555; text-transform: uppercase; letter-spacing: 0.08em; padding: 4px 0 8px; }
  .history-item { padding: 10px 12px; border-radius: 8px; cursor: pointer; border: 1px solid transparent; }
  .history-item:hover { background: #1e1e30; border-color: #2a2a3e; }
  .history-item.active { background: #1e1e40; border-color: #4f46e5; }
  .history-prompt { font-size: 13px; color: #ccc; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .history-meta { font-size: 11px; color: #555; margin-top: 3px; display: flex; gap: 6px; align-items: center; }
  .badge-sm { display: inline-block; padding: 1px 7px; border-radius: 10px; font-size: 10px; font-weight: 500; }
  .badge-sm.allowed { background: #14532d; color: #4ade80; }
  .badge-sm.blocked { background: #450a0a; color: #f87171; }
  .badge-sm.error { background: #451a03; color: #fb923c; }
  .badge-sm.review_pending { background: #3a2a00; color: #fbbf24; }
  .risk-badge { display: inline-block; padding: 1px 6px; border-radius: 10px; font-size: 10px; }
  .risk-minimal { background: #1e3a2e; color: #4ade80; }
  .risk-limited { background: #3a3010; color: #fbbf24; }
  .risk-high { background: #3a1010; color: #f87171; }
  .risk-unacceptable { background: #4a0a2e; color: #f472b6; }
  .chat-area { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
  .messages { flex: 1; overflow-y: auto; padding: 24px 32px; display: flex; flex-direction: column; gap: 20px; }
  .welcome { text-align: center; margin: auto; max-width: 500px; }
  .welcome h2 { font-size: 24px; font-weight: 500; color: #fff; margin-bottom: 10px; }
  .welcome p { font-size: 14px; color: #666; line-height: 1.7; margin-bottom: 24px; }
  .eu-badge { display: inline-block; background: #1a2a4e; color: #93c5fd; border: 1px solid #1e3a6e; border-radius: 6px; padding: 4px 12px; font-size: 12px; margin-bottom: 16px; }
  .suggestion-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
  .suggestion { background: #1a1a2e; border: 1px solid #2a2a3e; border-radius: 10px; padding: 12px 14px; font-size: 13px; color: #aaa; cursor: pointer; text-align: left; }
  .suggestion:hover { border-color: #4f46e5; color: #fff; background: #1e1e40; }
  .msg { display: flex; flex-direction: column; gap: 6px; max-width: 780px; }
  .msg.user { align-self: flex-end; align-items: flex-end; }
  .msg.assistant { align-self: flex-start; align-items: flex-start; }
  .msg-bubble { padding: 12px 16px; border-radius: 12px; font-size: 14px; line-height: 1.7; }
  .msg.user .msg-bubble { background: #4f46e5; color: #fff; border-bottom-right-radius: 3px; }
  .msg.assistant .msg-bubble { background: #1a1a2e; color: #ddd; border-bottom-left-radius: 3px; border: 1px solid #2a2a3e; }
  .msg.blocked-msg .msg-bubble { background: #1f0a0a; border: 1px solid #450a0a; color: #f87171; }
  .msg.review-msg .msg-bubble { background: #1a1500; border: 1px solid #4a3800; color: #fbbf24; }
  .msg-meta { font-size: 11px; color: #555; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .reasoning-toggle { font-size: 11px; color: #4f46e5; cursor: pointer; background: none; border: none; padding: 0; }
  .reasoning-box { background: #0d0d1a; border: 1px solid #2a2a3e; border-radius: 8px; padding: 10px 14px; font-size: 12px; color: #888; line-height: 1.6; margin-top: 4px; display: none; max-width: 700px; }
  .reasoning-box.open { display: block; }
  .typing { display: flex; gap: 5px; align-items: center; padding: 14px 16px; background: #1a1a2e; border-radius: 12px; border: 1px solid #2a2a3e; border-bottom-left-radius: 3px; }
  .dot { width: 7px; height: 7px; border-radius: 50%; background: #4f46e5; animation: bounce 1.2s infinite; }
  .dot:nth-child(2) { animation-delay: 0.2s; }
  .dot:nth-child(3) { animation-delay: 0.4s; }
  @keyframes bounce { 0%,60%,100%{transform:translateY(0)} 30%{transform:translateY(-6px)} }
  .waiting-review { display: flex; gap: 10px; align-items: center; padding: 14px 16px; background: #1a1500; border-radius: 12px; border: 1px solid #4a3800; border-bottom-left-radius: 3px; font-size: 13px; color: #fbbf24; }
  .spinner { width: 16px; height: 16px; border: 2px solid #4a3800; border-top-color: #fbbf24; border-radius: 50%; animation: spin 1s linear infinite; flex-shrink: 0; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .input-area { padding: 16px 32px 24px; border-top: 1px solid #1e1e30; background: #0f0f1a; }
  .input-row { display: flex; gap: 10px; align-items: flex-end; background: #1a1a2e; border: 1px solid #2a2a3e; border-radius: 14px; padding: 10px 14px; }
  .input-row:focus-within { border-color: #4f46e5; }
  textarea { flex: 1; background: transparent; border: none; outline: none; color: #e0e0e0; font-size: 14px; resize: none; min-height: 24px; max-height: 120px; font-family: inherit; line-height: 1.5; }
  textarea::placeholder { color: #444; }
  .send-btn { width: 36px; height: 36px; background: #4f46e5; border: none; border-radius: 8px; cursor: pointer; display: flex; align-items: center; justify-content: center; flex-shrink: 0; }
  .send-btn:hover { background: #4338ca; }
  .send-btn:disabled { background: #2a2a3e; cursor: not-allowed; }
  .send-btn svg { width: 16px; height: 16px; fill: white; }
  .hint { font-size: 11px; color: #444; text-align: center; margin-top: 8px; }
</style>
</head>
<body>
<div class="topbar">
  <h1>AI Gateway — EU AI Act Compliant</h1>
  <div class="topbar-right">
    <input class="key-input" id="apiKeyInput" type="password" placeholder="Paste your API key..." />
    <a href="/dashboard" class="nav-link">Audit Log</a>
    <a href="/review" class="nav-link" style="color:#fbbf24">Review Queue</a>
  </div>
</div>
<div class="main">
  <div class="sidebar">
    <div class="sidebar-title">Recent prompts</div>
    <div id="historyList"></div>
  </div>
  <div class="chat-area">
    <div class="messages" id="messages">
      <div class="welcome" id="welcome">
        <div class="eu-badge">EU AI Act compliant — Articles 5, 9, 13, 14, 52</div>
        <h2>AI Gateway</h2>
        <p>Every request is authenticated, classified for intent with full reasoning chain, risk-scored against the EU AI Act, and logged with complete auditability. Sensitive or borderline requests go to the human review queue.</p>
        <div class="suggestion-grid">
          <button class="suggestion" onclick="useSuggestion(this)">What is machine learning?</button>
          <button class="suggestion" onclick="useSuggestion(this)">Explain Docker in simple terms</button>
          <button class="suggestion" onclick="useSuggestion(this)">What is a REST API?</button>
          <button class="suggestion" onclick="useSuggestion(this)">How does rate limiting work?</button>
        </div>
      </div>
    </div>
    <div class="input-area">
      <div class="input-row">
        <textarea id="promptInput" placeholder="Type a prompt and press Enter..." rows="1"></textarea>
        <button class="send-btn" id="sendBtn" onclick="sendPromptToGateway()" title="Send">
          <svg viewBox="0 0 24 24"><path d="M2 21L23 12 2 3v7l15 2-15 2z"/></svg>
        </button>
      </div>
      <div class="hint">Enter to send &nbsp;·&nbsp; Shift+Enter for new line</div>
    </div>
  </div>
</div>
<script>
  const history = [];
  const textarea = document.getElementById('promptInput');
  textarea.addEventListener('input', () => {
    textarea.style.height = 'auto';
    textarea.style.height = Math.min(textarea.scrollHeight, 120) + 'px';
  });
  textarea.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendPromptToGateway(); }
  });
  function useSuggestion(btn) { textarea.value = btn.textContent; textarea.focus(); }
  function getApiKey() { return document.getElementById('apiKeyInput').value.trim(); }

  function removeWelcome() {
    const w = document.getElementById('welcome');
    if (w) w.remove();
  }

  function addMessage(role, text, meta) {
    removeWelcome();
    const messages = document.getElementById('messages');
    const div = document.createElement('div');
    let cls = 'msg ' + role;
    if (meta && meta.status === 'blocked') cls += ' blocked-msg';
    if (meta && meta.status === 'review_pending') cls += ' review-msg';
    div.className = cls;

    const bubble = document.createElement('div');
    bubble.className = 'msg-bubble';
    bubble.textContent = text;
    div.appendChild(bubble);

    if (meta) {
      const metaDiv = document.createElement('div');
      metaDiv.className = 'msg-meta';
      metaDiv.innerHTML = new Date().toLocaleTimeString();
      if (meta.status) metaDiv.innerHTML += ' &nbsp;·&nbsp; <span class="badge-sm ' + meta.status + '">' + meta.status + '</span>';
      if (meta.risk_level) metaDiv.innerHTML += ' &nbsp;·&nbsp; <span class="risk-badge risk-' + meta.risk_level + '">' + meta.risk_level + ' risk</span>';
      if (meta.eu_article) metaDiv.innerHTML += ' &nbsp;·&nbsp; ' + meta.eu_article;
      div.appendChild(metaDiv);

      if (meta.reasoning_chain) {
        const btn = document.createElement('button');
        btn.className = 'reasoning-toggle';
        btn.textContent = 'View reasoning chain ↓';
        const box = document.createElement('div');
        box.className = 'reasoning-box';
        box.textContent = meta.reasoning_chain;
        btn.onclick = () => {
          box.classList.toggle('open');
          btn.textContent = box.classList.contains('open') ? 'Hide reasoning chain ↑' : 'View reasoning chain ↓';
        };
        div.appendChild(btn);
        div.appendChild(box);
      }
    }
    messages.appendChild(div);
    messages.scrollTop = messages.scrollHeight;
    return div;
  }

  function addTyping() {
    removeWelcome();
    const messages = document.getElementById('messages');
    const div = document.createElement('div');
    div.className = 'msg assistant';
    div.id = 'typing-indicator';
    div.innerHTML = '<div class="typing"><div class="dot"></div><div class="dot"></div><div class="dot"></div></div>';
    messages.appendChild(div);
    messages.scrollTop = messages.scrollHeight;
  }

  function addWaitingReview() {
    removeWelcome();
    const messages = document.getElementById('messages');
    const div = document.createElement('div');
    div.className = 'msg assistant';
    div.id = 'typing-indicator';
    div.innerHTML = '<div class="waiting-review"><div class="spinner"></div><span>Waiting for human review — <a href="/review" target="_blank" style="color:#fbbf24;font-weight:500">open review queue ↗</a> to approve or reject</span></div>';
    messages.appendChild(div);
    messages.scrollTop = messages.scrollHeight;
  }

  function removeTyping() {
    const t = document.getElementById('typing-indicator');
    if (t) t.remove();
  }

  function addToHistory(prompt, status, riskLevel) {
    history.unshift({ prompt, status, riskLevel });
    const list = document.getElementById('historyList');
    list.innerHTML = '';
    history.slice(0, 20).forEach((item, i) => {
      const div = document.createElement('div');
      div.className = 'history-item' + (i === 0 ? ' active' : '');
      div.innerHTML =
        '<div class="history-prompt">' + item.prompt + '</div>' +
        '<div class="history-meta">' +
        '<span class="badge-sm ' + item.status + '">' + item.status + '</span>' +
        (item.riskLevel ? '<span class="risk-badge risk-' + item.riskLevel + '">' + item.riskLevel + '</span>' : '') +
        '</div>';
      div.onclick = () => { textarea.value = item.prompt; textarea.focus(); };
      list.appendChild(div);
    });
  }

  async function sendPromptToGateway() {
    const prompt = textarea.value.trim();
    if (!prompt) return;
    const apiKey = getApiKey();
    if (!apiKey) {
      alert('Please paste your API key in the top right field.\n\nValid keys: key-alpha-123, key-beta-456, key-gamma-789');
      return;
    }
    textarea.value = '';
    textarea.style.height = 'auto';
    document.getElementById('sendBtn').disabled = true;
    addMessage('user', prompt, null);

    // Show appropriate waiting indicator
    const isLikelySensitive = ['repeat', 'instructions', 'system prompt', 'told', 'pretend', 'inject'].some(w => prompt.toLowerCase().includes(w));
    if (isLikelySensitive) {
      addWaitingReview();
    } else {
      addTyping();
    }

    try {
      const res = await fetch('/ai', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-API-Key': apiKey },
        body: JSON.stringify({ prompt })
      });
      const data = await res.json();
      removeTyping();

      if (res.status === 401) {
        addMessage('assistant', 'Invalid or missing API key.', { status: 'error' });
        addToHistory(prompt, 'error', null);
      } else if (res.status === 451) {
        addMessage('assistant',
          'Blocked — EU AI Act Article 5 violation\n\n' + data.reason,
          { status: 'blocked', risk_level: 'unacceptable', eu_article: data.article });
        addToHistory(prompt, 'blocked', 'unacceptable');
      } else if (res.status === 403) {
        addMessage('assistant',
          'Blocked: ' + data.reason,
          { status: 'blocked', risk_level: data.risk_level, eu_article: data.eu_article, reasoning_chain: data.reasoning_chain });
        addToHistory(prompt, 'blocked', data.risk_level);
      } else if (res.ok) {
        addMessage('assistant', data.response, { status: 'allowed', risk_level: data.risk_level });
        addToHistory(prompt, 'allowed', data.risk_level);
      } else {
        addMessage('assistant', 'Error: ' + (data.reason || 'something went wrong'), { status: 'error' });
        addToHistory(prompt, 'error', null);
      }
    } catch (err) {
      removeTyping();
      addMessage('assistant', 'Could not reach the gateway. Is it running?', { status: 'error' });
      addToHistory(prompt, 'error', null);
    }
    document.getElementById('sendBtn').disabled = false;
    textarea.focus();
  }
</script>
</body>
</html>`

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AI Gateway — Audit Dashboard</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #f5f5f5; color: #333; }
  .header { background: #1a1a2e; color: white; padding: 20px 32px; display: flex; justify-content: space-between; align-items: center; }
  .header h1 { font-size: 18px; font-weight: 500; }
  .header-right { display: flex; align-items: center; gap: 12px; }
  .auto-badge { font-size: 11px; background: #16a34a; color: white; padding: 3px 10px; border-radius: 20px; }
  .eu-badge { font-size: 11px; background: #1e3a6e; color: #93c5fd; padding: 3px 10px; border-radius: 20px; }
  .nav-link { font-size: 13px; color: #aaa; text-decoration: none; padding: 6px 14px; border: 1px solid #3a3a5e; border-radius: 6px; }
  .nav-link:hover { color: #fff; }
  .nav-link.review { border-color: #7a6000; color: #fbbf24; }
  .container { max-width: 1400px; margin: 0 auto; padding: 24px 32px; }
  .stats { display: grid; grid-template-columns: repeat(6, 1fr); gap: 12px; margin-bottom: 24px; }
  .stat-card { background: white; border-radius: 10px; padding: 16px; border: 1px solid #eee; }
  .stat-card .label { font-size: 11px; color: #888; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 6px; }
  .stat-card .value { font-size: 28px; font-weight: 600; }
  .stat-card.total .value { color: #1a1a2e; }
  .stat-card.allowed .value { color: #16a34a; }
  .stat-card.blocked .value { color: #dc2626; }
  .stat-card.errors .value { color: #d97706; }
  .stat-card.highrisk .value { color: #dc2626; }
  .stat-card.unacceptable .value { color: #9333ea; }
  .chart-wrap { background: white; border-radius: 10px; border: 1px solid #eee; padding: 20px; margin-bottom: 24px; }
  .chart-title { font-size: 13px; font-weight: 500; color: #666; margin-bottom: 16px; }
  .chart { display: flex; align-items: flex-end; gap: 6px; height: 80px; }
  .bar-group { display: flex; flex-direction: column; align-items: center; flex: 1; gap: 4px; }
  .bar { width: 100%; background: #4f46e5; border-radius: 3px 3px 0 0; min-height: 2px; }
  .bar-label { font-size: 10px; color: #aaa; }
  .no-chart { color: #aaa; font-size: 13px; text-align: center; padding: 20px 0; }
  .toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; flex-wrap: wrap; gap: 10px; }
  .search-wrap { display: flex; gap: 8px; flex-wrap: wrap; }
  .search-wrap input, .search-wrap select { padding: 8px 14px; border: 1px solid #ddd; border-radius: 8px; font-size: 13px; outline: none; }
  .search-wrap input:focus, .search-wrap select:focus { border-color: #4f46e5; }
  .search-wrap button { padding: 8px 16px; background: #4f46e5; color: white; border: none; border-radius: 8px; font-size: 13px; cursor: pointer; }
  .clear-btn { padding: 8px 14px; background: white; color: #666; border: 1px solid #ddd; border-radius: 8px; font-size: 13px; cursor: pointer; text-decoration: none; }
  .section-title { font-size: 15px; font-weight: 500; color: #444; }
  .table-wrap { background: white; border-radius: 10px; border: 1px solid #eee; overflow: hidden; overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font-size: 12px; min-width: 900px; }
  thead { background: #f9f9f9; }
  th { padding: 10px 12px; text-align: left; font-weight: 500; color: #666; font-size: 11px; text-transform: uppercase; letter-spacing: 0.04em; border-bottom: 1px solid #eee; }
  td { padding: 10px 12px; border-bottom: 1px solid #f0f0f0; vertical-align: top; max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  tr:last-child td { border-bottom: none; }
  tr:hover td { background: #fafafa; }
  .badge { display: inline-block; padding: 2px 8px; border-radius: 20px; font-size: 11px; font-weight: 500; }
  .badge.allowed { background: #dcfce7; color: #16a34a; }
  .badge.blocked { background: #fee2e2; color: #dc2626; }
  .badge.error { background: #fef3c7; color: #d97706; }
  .badge.review_pending { background: #fef3c7; color: #b45309; }
  .risk-badge { display: inline-block; padding: 2px 8px; border-radius: 20px; font-size: 11px; font-weight: 500; }
  .risk-minimal { background: #dcfce7; color: #16a34a; }
  .risk-limited { background: #fef3c7; color: #d97706; }
  .risk-high { background: #fee2e2; color: #dc2626; }
  .risk-unacceptable { background: #f3e8ff; color: #9333ea; }
  .detail-link { color: #4f46e5; text-decoration: none; font-size: 11px; }
  .detail-link:hover { text-decoration: underline; }
  .empty { text-align: center; padding: 48px; color: #aaa; font-size: 14px; }
  .reasoning-preview { font-size: 11px; color: #999; max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
</head>
<body>
<div class="header">
  <h1>AI Gateway — Audit Dashboard</h1>
  <div class="header-right">
    <span class="eu-badge">EU AI Act compliant</span>
    <span class="auto-badge">Auto-refresh ON</span>
    <span id="countdown" style="font-size:13px;opacity:0.6">Refreshing in 10s</span>
    <a href="/review" class="nav-link review">Review Queue</a>
    <a href="/" class="nav-link">Chat UI</a>
  </div>
</div>
<div class="container">
  <div class="stats">
    <div class="stat-card total"><div class="label">Total</div><div class="value">{{.Stats.Total}}</div></div>
    <div class="stat-card allowed"><div class="label">Allowed</div><div class="value">{{.Stats.Allowed}}</div></div>
    <div class="stat-card blocked"><div class="label">Blocked</div><div class="value">{{.Stats.Blocked}}</div></div>
    <div class="stat-card errors"><div class="label">Errors</div><div class="value">{{.Stats.Errors}}</div></div>
    <div class="stat-card highrisk"><div class="label">High risk</div><div class="value">{{.Stats.HighRisk}}</div></div>
    <div class="stat-card unacceptable"><div class="label">Unacceptable</div><div class="value">{{.Stats.Unacceptable}}</div></div>
  </div>
  <div class="chart-wrap">
    <div class="chart-title">Requests by hour (last 24 hours)</div>
    {{if .Buckets}}
    <div class="chart">
      {{range .Buckets}}
      <div class="bar-group">
        <div class="bar" style="height:{{.Count}}0px" title="{{.Count}} requests"></div>
        <div class="bar-label">{{.Hour}}</div>
      </div>
      {{end}}
    </div>
    {{else}}
    <div class="no-chart">No data yet for the last 24 hours.</div>
    {{end}}
  </div>
  <div class="toolbar">
    <p class="section-title">Audit log — {{len .Logs}} entries</p>
    <form class="search-wrap" method="GET" action="/dashboard">
      <input type="text" name="search" placeholder="Search prompt, status, category, IP..." value="{{.Search}}">
      <select name="risk">
        <option value="" {{if eq .RiskFilter ""}}selected{{end}}>All risk levels</option>
        <option value="minimal" {{if eq .RiskFilter "minimal"}}selected{{end}}>Minimal</option>
        <option value="limited" {{if eq .RiskFilter "limited"}}selected{{end}}>Limited</option>
        <option value="high" {{if eq .RiskFilter "high"}}selected{{end}}>High</option>
        <option value="unacceptable" {{if eq .RiskFilter "unacceptable"}}selected{{end}}>Unacceptable</option>
      </select>
      <button type="submit">Filter</button>
      {{if or .Search .RiskFilter}}<a href="/dashboard" class="clear-btn">Clear</a>{{end}}
    </form>
  </div>
  <div class="table-wrap">
    {{if .Logs}}
    <table>
      <thead>
        <tr><th>#</th><th>Time</th><th>IP</th><th>Prompt</th><th>Status</th><th>Risk</th><th>Category</th><th>Score</th><th>EU Article</th><th>Reasoning</th><th>Detail</th></tr>
      </thead>
      <tbody>
        {{range .Logs}}
        <tr>
          <td>{{.ID}}</td>
          <td>{{.Timestamp}}</td>
          <td title="{{.ClientIP}}">{{.ClientIP}}</td>
          <td title="{{.Prompt}}">{{.Prompt}}</td>
          <td><span class="badge {{.Status}}">{{.Status}}</span></td>
          <td><span class="risk-badge risk-{{.RiskLevel}}">{{.RiskLevel}}</span></td>
          <td>{{.Category}}</td>
          <td>{{printf "%.2f" .ClassifierScore}}</td>
          <td>{{.EUArticle}}</td>
          <td><div class="reasoning-preview" title="{{.ReasoningChain}}">{{.ReasoningChain}}</div></td>
          <td><a href="/admin/audit/{{.ID}}" class="detail-link">View →</a></td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{else}}
    <div class="empty">{{if or .Search .RiskFilter}}No results for the current filter.{{else}}No requests logged yet.{{end}}</div>
    {{end}}
  </div>
</div>
<script>
  let s = 10;
  const el = document.getElementById('countdown');
  setInterval(() => {
    s--;
    if (s <= 0) {
      const p = new URLSearchParams(window.location.search);
      window.location.href = '/dashboard' + (p.toString() ? '?' + p.toString() : '');
    }
    el.textContent = 'Refreshing in ' + s + 's';
  }, 1000);
</script>
</body>
</html>`

// HandleReviewPage serves the human review queue UI at /review.
// Operators open this page to see pending items and click Approve or Reject.
// The page polls /admin/review every 3 seconds for live updates.
func (d *Dashboard) HandleReviewPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(reviewPageHTML))
}

const reviewPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AI Gateway — Review Queue</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #0f0f1a; color: #e0e0e0; min-height: 100vh; }
  .header { background: #1a1a2e; padding: 16px 32px; display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid #2a2a3e; }
  .header h1 { font-size: 16px; font-weight: 500; color: #fff; }
  .header-right { display: flex; gap: 12px; align-items: center; }
  .nav-link { font-size: 13px; color: #888; text-decoration: none; padding: 6px 12px; border-radius: 6px; border: 1px solid #2a2a3e; }
  .nav-link:hover { color: #fff; border-color: #4f46e5; }
  .pulse { width: 8px; height: 8px; border-radius: 50%; background: #fbbf24; animation: pulse 2s infinite; }
  @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.3} }
  .live-label { font-size: 12px; color: #fbbf24; display: flex; align-items: center; gap: 6px; }
  .container { max-width: 1000px; margin: 0 auto; padding: 32px; }
  .page-title { font-size: 20px; font-weight: 500; color: #fff; margin-bottom: 6px; }
  .page-sub { font-size: 13px; color: #666; margin-bottom: 28px; line-height: 1.6; }
  .article-ref { display: inline-block; background: #1a2a4e; color: #93c5fd; border-radius: 4px; padding: 2px 8px; font-size: 11px; margin-left: 8px; }
  .stats-row { display: flex; gap: 12px; margin-bottom: 28px; flex-wrap: wrap; }
  .stat { background: #1a1a2e; border: 1px solid #2a2a3e; border-radius: 10px; padding: 14px 20px; min-width: 100px; text-align: center; }
  .stat-val { font-size: 26px; font-weight: 600; }
  .stat-label { font-size: 11px; color: #666; margin-top: 3px; text-transform: uppercase; letter-spacing: 0.05em; }
  .stat.pending .stat-val { color: #fbbf24; }
  .stat.approved .stat-val { color: #4ade80; }
  .stat.rejected .stat-val { color: #f87171; }
  .stat.expired .stat-val { color: #666; }
  .section-title { font-size: 13px; font-weight: 500; color: #888; text-transform: uppercase; letter-spacing: 0.06em; margin-bottom: 14px; }
  .empty { background: #1a1a2e; border: 1px solid #2a2a3e; border-radius: 12px; padding: 48px; text-align: center; color: #555; font-size: 14px; }
  .empty-icon { font-size: 32px; margin-bottom: 12px; }
  .item { background: #1a1a2e; border: 1px solid #2a2a3e; border-radius: 12px; padding: 20px; margin-bottom: 14px; }
  .item.urgent { border-color: #7a4a00; }
  .item-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 14px; gap: 16px; }
  .item-meta { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .item-id { font-size: 11px; color: #555; }
  .category-badge { font-size: 11px; padding: 2px 10px; border-radius: 20px; font-weight: 500; }
  .category-data_extraction { background: #1a2a4e; color: #93c5fd; }
  .category-identity_manipulation { background: #2a1a4e; color: #c4b5fd; }
  .category-prompt_injection { background: #2a1a1a; color: #fca5a5; }
  .category-jailbreak { background: #2a1a1a; color: #fca5a5; }
  .category-harmful_content { background: #3a0a0a; color: #f87171; }
  .category-safe { background: #0a2a1a; color: #4ade80; }
  .score-badge { font-size: 11px; padding: 2px 10px; border-radius: 20px; background: #2a2a3e; color: #aaa; }
  .score-high { background: #3a1010; color: #f87171; }
  .score-mid { background: #3a3010; color: #fbbf24; }
  .score-low { background: #0a2a1a; color: #4ade80; }
  .expires { font-size: 11px; color: #f87171; }
  .prompt-box { background: #0f0f1a; border: 1px solid #2a2a3e; border-radius: 8px; padding: 12px 14px; font-size: 13px; color: #ddd; line-height: 1.6; margin-bottom: 12px; white-space: pre-wrap; word-break: break-word; }
  .reasoning-box { background: #0a0a14; border: 1px solid #1e1e30; border-radius: 8px; padding: 10px 14px; font-size: 12px; color: #666; line-height: 1.6; margin-bottom: 14px; display: none; }
  .reasoning-box.open { display: block; }
  .reasoning-toggle { font-size: 12px; color: #4f46e5; cursor: pointer; background: none; border: none; padding: 0; margin-bottom: 12px; }
  .actions { display: flex; gap: 10px; }
  .btn-approve { padding: 9px 24px; background: #16a34a; color: #fff; border: none; border-radius: 8px; font-size: 13px; font-weight: 500; cursor: pointer; }
  .btn-approve:hover { background: #15803d; }
  .btn-approve:disabled { background: #1a3a2e; color: #555; cursor: not-allowed; }
  .btn-reject { padding: 9px 24px; background: #dc2626; color: #fff; border: none; border-radius: 8px; font-size: 13px; font-weight: 500; cursor: pointer; }
  .btn-reject:hover { background: #b91c1c; }
  .btn-reject:disabled { background: #3a1010; color: #555; cursor: not-allowed; }
  .decided-badge { font-size: 12px; padding: 4px 14px; border-radius: 8px; font-weight: 500; }
  .decided-approved { background: #14532d; color: #4ade80; }
  .decided-rejected { background: #450a0a; color: #f87171; }
  .decided-expired { background: #2a2a2a; color: #666; }
  .decided-by { font-size: 11px; color: #555; margin-top: 6px; }
  .history-section { margin-top: 40px; }
  .history-item { background: #13131f; border: 1px solid #1e1e2e; border-radius: 10px; padding: 14px 16px; margin-bottom: 8px; display: flex; justify-content: space-between; align-items: center; gap: 12px; }
  .history-prompt { font-size: 13px; color: #888; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; flex: 1; }
  .history-meta { display: flex; gap: 8px; align-items: center; flex-shrink: 0; }
</style>
</head>
<body>
<div class="header">
  <h1>Human Review Queue <span class="article-ref">EU AI Act Article 14</span></h1>
  <div class="header-right">
    <div class="live-label"><div class="pulse"></div>Live — refreshes every 3s</div>
    <a href="/dashboard" class="nav-link">Audit Log</a>
    <a href="/" class="nav-link">Chat UI</a>
  </div>
</div>

<div class="container">
  <p class="page-title">Pending Review Items</p>
  <p class="page-sub">Requests where the AI classifier identified a sensitive category (data extraction, identity manipulation, prompt injection) or scored in the uncertain zone (0.4–0.75) are held here for human judgment before proceeding. Unanswered items expire in 5 minutes and are blocked by default per the EU AI Act Article 14 precautionary principle.</p>

  <div class="stats-row" id="statsRow">
    <div class="stat pending"><div class="stat-val" id="statPending">—</div><div class="stat-label">Pending</div></div>
    <div class="stat approved"><div class="stat-val" id="statApproved">—</div><div class="stat-label">Approved</div></div>
    <div class="stat rejected"><div class="stat-val" id="statRejected">—</div><div class="stat-label">Rejected</div></div>
    <div class="stat expired"><div class="stat-val" id="statExpired">—</div><div class="stat-label">Expired</div></div>
  </div>

  <div class="section-title">Pending — awaiting decision</div>
  <div id="pendingList"></div>

  <div class="history-section">
    <div class="section-title">Recent decisions</div>
    <div id="historyList"></div>
  </div>
</div>

<script>
  let deciding = {};

  function scoreClass(score) {
    if (score >= 0.7) return 'score-high';
    if (score >= 0.4) return 'score-mid';
    return 'score-low';
  }

  function timeUntil(expiresAt) {
    const exp = new Date(expiresAt.replace(' ', 'T') + 'Z');
    const diff = Math.max(0, Math.floor((exp - Date.now()) / 1000));
    if (diff <= 0) return 'expired';
    const m = Math.floor(diff / 60);
    const s = diff % 60;
    return m > 0 ? m + 'm ' + s + 's' : s + 's';
  }

  function categoryClass(cat) {
    return 'category-badge category-' + (cat || 'safe');
  }

  function renderPending(items) {
    const el = document.getElementById('pendingList');
    if (!items || items.length === 0) {
      el.innerHTML = '<div class="empty"><div class="empty-icon">✓</div>No pending items — the queue is clear</div>';
      return;
    }
    el.innerHTML = items.map(item => {
      const until = timeUntil(item.expires_at);
      const urgent = until !== 'expired' && parseInt(until) < 60;
      return '<div class="item' + (urgent ? ' urgent' : '') + '" id="item-' + item.id + '">' +
        '<div class="item-header">' +
          '<div class="item-meta">' +
            '<span class="item-id">#' + item.id + '</span>' +
            '<span class="' + categoryClass(item.category) + '">' + (item.category || 'unknown') + '</span>' +
            '<span class="score-badge ' + scoreClass(item.score) + '">score ' + item.score.toFixed(2) + '</span>' +
            '<span class="expires">expires in ' + until + '</span>' +
          '</div>' +
        '</div>' +
        '<div class="prompt-box">' + escHtml(item.prompt) + '</div>' +
        (item.reasoning ? '<button class="reasoning-toggle" onclick="toggleReason(this)">View AI reasoning ↓</button><div class="reasoning-box">' + escHtml(item.reasoning) + '</div>' : '') +
        '<div class="actions">' +
          '<button class="btn-approve" onclick="decide(' + item.id + ', \'approve\')" ' + (deciding[item.id] ? 'disabled' : '') + '>✓ Approve — allow through</button>' +
          '<button class="btn-reject" onclick="decide(' + item.id + ', \'reject\')" ' + (deciding[item.id] ? 'disabled' : '') + '>✗ Reject — block this request</button>' +
        '</div>' +
      '</div>';
    }).join('');
  }

  function renderHistory(items) {
    const el = document.getElementById('historyList');
    const decided = (items || []).filter(i => i.status !== 'pending');
    if (decided.length === 0) {
      el.innerHTML = '<div style="color:#555;font-size:13px">No decisions made yet</div>';
      return;
    }
    el.innerHTML = decided.slice(0, 15).map(item => {
      const statusClass = 'decided-' + item.status;
      const reviewer = item.reviewer ? ' by ' + item.reviewer : '';
      return '<div class="history-item">' +
        '<div class="history-prompt" title="' + escHtml(item.prompt) + '">' + escHtml(item.prompt) + '</div>' +
        '<div class="history-meta">' +
          '<span class="' + categoryClass(item.category) + '">' + (item.category || '') + '</span>' +
          '<span class="decided-badge ' + statusClass + '">' + item.status + '</span>' +
        '</div>' +
        (item.reviewer ? '<div class="decided-by">' + item.status + reviewer + '</div>' : '') +
      '</div>';
    }).join('');
  }

  function escHtml(str) {
    return (str || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
  }

  function toggleReason(btn) {
    const box = btn.nextElementSibling;
    box.classList.toggle('open');
    btn.textContent = box.classList.contains('open') ? 'Hide AI reasoning ↑' : 'View AI reasoning ↓';
  }

  async function decide(id, action) {
    deciding[id] = true;
    // Re-render to disable buttons immediately
    await refresh();

    const reviewer = 'admin';
    try {
      const res = await fetch('/admin/review/' + action, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: id, reviewer: reviewer })
      });
      const data = await res.json();
      if (res.ok) {
        console.log('Decision recorded:', data);
      } else {
        alert('Error: ' + data.reason);
      }
    } catch (err) {
      alert('Request failed: ' + err.message);
    }

    deciding[id] = false;
    await refresh();
  }

  async function refresh() {
    try {
      const [pendingRes, allRes, statsRes] = await Promise.all([
        fetch('/admin/review'),
        fetch('/admin/review/all'),
        fetch('/admin/review/stats')
      ]);

      const pending = await pendingRes.json();
      const all = await allRes.json();
      const stats = await statsRes.json();

      renderPending(pending.items || []);
      renderHistory(all.items || []);

      if (stats.stats) {
        document.getElementById('statPending').textContent = stats.stats.pending || 0;
        document.getElementById('statApproved').textContent = stats.stats.approved || 0;
        document.getElementById('statRejected').textContent = stats.stats.rejected || 0;
        document.getElementById('statExpired').textContent = stats.stats.expired || 0;
      }
    } catch (err) {
      console.error('Refresh error:', err);
    }
  }

  // Initial load + poll every 3 seconds
  refresh();
  setInterval(refresh, 3000);
</script>
</body>
</html>`

// HandleIncidentsPage serves the live incidents dashboard at /incidents
func (d *Dashboard) HandleIncidentsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(incidentsPageHTML))
}

const incidentsPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AI Gateway — Incidents</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #0f0f1a; color: #e0e0e0; min-height: 100vh; }
  .header { background: #1a1a2e; padding: 16px 32px; display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid #2a2a3e; }
  .header h1 { font-size: 16px; font-weight: 500; color: #fff; }
  .header-right { display: flex; gap: 12px; align-items: center; }
  .nav-link { font-size: 13px; color: #888; text-decoration: none; padding: 6px 12px; border-radius: 6px; border: 1px solid #2a2a3e; }
  .nav-link:hover { color: #fff; border-color: #4f46e5; }
  .pulse { width: 8px; height: 8px; border-radius: 50%; background: #f87171; animation: pulse 2s infinite; }
  @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.3} }
  .live-label { font-size: 12px; color: #f87171; display: flex; align-items: center; gap: 6px; }
  .container { max-width: 1100px; margin: 0 auto; padding: 32px; }
  .page-title { font-size: 20px; font-weight: 500; color: #fff; margin-bottom: 6px; }
  .page-sub { font-size: 13px; color: #666; margin-bottom: 28px; line-height: 1.6; }
  .stats-row { display: grid; grid-template-columns: repeat(6, 1fr); gap: 12px; margin-bottom: 28px; }
  .stat { background: #1a1a2e; border: 1px solid #2a2a3e; border-radius: 10px; padding: 14px 16px; text-align: center; }
  .stat-val { font-size: 26px; font-weight: 600; }
  .stat-label { font-size: 11px; color: #666; margin-top: 3px; text-transform: uppercase; letter-spacing: 0.05em; }
  .stat.total .stat-val { color: #e0e0e0; }
  .stat.unresolved .stat-val { color: #f87171; }
  .stat.critical .stat-val { color: #f472b6; }
  .stat.high .stat-val { color: #f87171; }
  .stat.medium .stat-val { color: #fbbf24; }
  .stat.low .stat-val { color: #4ade80; }
  .toolbar { display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px; flex-wrap: wrap; gap: 10px; }
  .filter-row { display: flex; gap: 8px; }
  .filter-btn { padding: 6px 14px; background: #1a1a2e; border: 1px solid #2a2a3e; border-radius: 20px; font-size: 12px; color: #888; cursor: pointer; }
  .filter-btn.active { border-color: #4f46e5; color: #fff; background: #1e1e40; }
  .section-title { font-size: 13px; font-weight: 500; color: #666; text-transform: uppercase; letter-spacing: 0.06em; }
  .empty { background: #1a1a2e; border: 1px solid #2a2a3e; border-radius: 12px; padding: 48px; text-align: center; color: #555; font-size: 14px; }
  .incident { background: #1a1a2e; border: 1px solid #2a2a3e; border-radius: 12px; padding: 18px 20px; margin-bottom: 10px; }
  .incident.critical { border-left: 3px solid #f472b6; }
  .incident.high { border-left: 3px solid #f87171; }
  .incident.medium { border-left: 3px solid #fbbf24; }
  .incident.low { border-left: 3px solid #4ade80; }
  .incident.resolved { opacity: 0.5; }
  .inc-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 12px; gap: 12px; }
  .inc-meta { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .inc-id { font-size: 11px; color: #555; }
  .severity-badge { font-size: 11px; padding: 2px 10px; border-radius: 20px; font-weight: 500; }
  .severity-critical { background: #4a0a2e; color: #f472b6; }
  .severity-high { background: #450a0a; color: #f87171; }
  .severity-medium { background: #3a2a00; color: #fbbf24; }
  .severity-low { background: #0a2a1a; color: #4ade80; }
  .category-badge { font-size: 11px; padding: 2px 10px; border-radius: 20px; background: #1e1e30; color: #888; }
  .article-badge { font-size: 11px; padding: 2px 10px; border-radius: 20px; background: #1a2a4e; color: #93c5fd; }
  .email-badge { font-size: 11px; padding: 2px 8px; border-radius: 20px; background: #0a2a1a; color: #4ade80; }
  .resolved-badge { font-size: 11px; padding: 2px 8px; border-radius: 20px; background: #1e2a1e; color: #4ade80; }
  .inc-time { font-size: 11px; color: #555; flex-shrink: 0; }
  .inc-prompt { background: #0f0f1a; border: 1px solid #1e1e2e; border-radius: 8px; padding: 10px 14px; font-size: 13px; color: #bbb; line-height: 1.6; margin-bottom: 10px; white-space: pre-wrap; word-break: break-word; }
  .inc-reason { font-size: 12px; color: #666; margin-bottom: 10px; }
  .inc-footer { display: flex; justify-content: space-between; align-items: center; }
  .inc-ip { font-size: 11px; color: #555; font-family: monospace; }
  .resolve-btn { padding: 6px 16px; background: #1e3a2e; border: 1px solid #16a34a; border-radius: 6px; color: #4ade80; font-size: 12px; cursor: pointer; }
  .resolve-btn:hover { background: #14532d; }
  .resolve-btn:disabled { opacity: 0.4; cursor: not-allowed; }
</style>
</head>
<body>
<div class="header">
  <h1>Security Incidents</h1>
  <div class="header-right">
    <div class="live-label"><div class="pulse"></div>Live — refreshes every 5s</div>
    <a href="/review" class="nav-link">Review Queue</a>
    <a href="/dashboard" class="nav-link">Audit Log</a>
    <a href="/" class="nav-link">Chat UI</a>
  </div>
</div>

<div class="container">
  <p class="page-title">Incident Dashboard</p>
  <p class="page-sub">Every high-confidence block (score ≥ 0.75), Article 5 violation, and human rejection creates an incident here. Critical and high severity incidents trigger an email alert via Resend. Resolve incidents after investigation.</p>

  <div class="stats-row" id="statsRow">
    <div class="stat total"><div class="stat-val" id="sTotal">—</div><div class="stat-label">Total</div></div>
    <div class="stat unresolved"><div class="stat-val" id="sUnresolved">—</div><div class="stat-label">Unresolved</div></div>
    <div class="stat critical"><div class="stat-val" id="sCritical">—</div><div class="stat-label">Critical</div></div>
    <div class="stat high"><div class="stat-val" id="sHigh">—</div><div class="stat-label">High</div></div>
    <div class="stat medium"><div class="stat-val" id="sMedium">—</div><div class="stat-label">Medium</div></div>
    <div class="stat low"><div class="stat-val" id="sLow">—</div><div class="stat-label">Low</div></div>
  </div>

  <div class="toolbar">
    <p class="section-title">All incidents</p>
    <div class="filter-row">
      <button class="filter-btn active" onclick="setFilter('', this)">All</button>
      <button class="filter-btn" onclick="setFilter('critical', this)">Critical</button>
      <button class="filter-btn" onclick="setFilter('high', this)">High</button>
      <button class="filter-btn" onclick="setFilter('medium', this)">Medium</button>
      <button class="filter-btn" onclick="setFilter('low', this)">Low</button>
    </div>
  </div>

  <div id="incidentsList"></div>
</div>

<script>
  let currentFilter = '';
  let resolving = {};

  function setFilter(severity, btn) {
    currentFilter = severity;
    document.querySelectorAll('.filter-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    refresh();
  }

  function esc(str) {
    return (str || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  }

  function renderIncidents(items) {
    const el = document.getElementById('incidentsList');
    if (!items || items.length === 0) {
      el.innerHTML = '<div class="empty">No incidents' + (currentFilter ? ' with severity "' + currentFilter + '"' : '') + '</div>';
      return;
    }

    el.innerHTML = items.map(inc => {
      const cls = 'incident ' + inc.severity + (inc.resolved ? ' resolved' : '');
      const displayPrompt = inc.prompt.length > 200 ? inc.prompt.substring(0, 200) + '...' : inc.prompt;

      return '<div class="' + cls + '">' +
        '<div class="inc-header">' +
          '<div class="inc-meta">' +
            '<span class="inc-id">#' + inc.id + '</span>' +
            '<span class="severity-badge severity-' + inc.severity + '">' + inc.severity.toUpperCase() + '</span>' +
            '<span class="category-badge">' + esc(inc.category) + '</span>' +
            (inc.eu_article ? '<span class="article-badge">' + esc(inc.eu_article) + '</span>' : '') +
            (inc.email_sent ? '<span class="email-badge">📧 email sent</span>' : '') +
            (inc.resolved ? '<span class="resolved-badge">✓ resolved by ' + esc(inc.resolved_by) + '</span>' : '') +
          '</div>' +
          '<span class="inc-time">' + esc(inc.timestamp) + '</span>' +
        '</div>' +
        '<div class="inc-prompt">' + esc(displayPrompt) + '</div>' +
        '<div class="inc-reason">' + esc(inc.reason) + '</div>' +
        '<div class="inc-footer">' +
          '<span class="inc-ip">' + esc(inc.client_ip) + '</span>' +
          (!inc.resolved ?
            '<button class="resolve-btn" onclick="resolve(' + inc.id + ', this)" ' + (resolving[inc.id] ? 'disabled' : '') + '>Mark resolved</button>'
            : ''
          ) +
        '</div>' +
      '</div>';
    }).join('');
  }

  async function resolve(id, btn) {
    resolving[id] = true;
    btn.disabled = true;
    btn.textContent = 'Resolving...';

    try {
      const res = await fetch('/admin/incidents/resolve', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: id, resolved_by: 'admin' })
      });
      const data = await res.json();
      if (!res.ok) alert('Error: ' + data.reason);
    } catch (err) {
      alert('Request failed: ' + err.message);
    }

    resolving[id] = false;
    await refresh();
  }

  async function refresh() {
    try {
      const url = '/admin/incidents' + (currentFilter ? '?severity=' + currentFilter : '');
      const [incRes, statsRes] = await Promise.all([
        fetch(url),
        fetch('/admin/incidents/stats')
      ]);

      const incData = await incRes.json();
      const statsData = await statsRes.json();

      renderIncidents(incData.incidents || []);

      if (statsData.stats) {
        const s = statsData.stats;
        document.getElementById('sTotal').textContent = s.total || 0;
        document.getElementById('sUnresolved').textContent = s.unresolved || 0;
        document.getElementById('sCritical').textContent = s.critical || 0;
        document.getElementById('sHigh').textContent = s.high || 0;
        document.getElementById('sMedium').textContent = s.medium || 0;
        document.getElementById('sLow').textContent = s.low || 0;
      }
    } catch (err) {
      console.error('Refresh error:', err);
    }
  }

  refresh();
  setInterval(refresh, 5000);
</script>
</body>
</html>`

// HandleRetentionPage serves the retention policy management UI at /retention
func (d *Dashboard) HandleRetentionPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(retentionPageHTML))
}

const retentionPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AI Gateway — Data Retention</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #0f0f1a; color: #e0e0e0; min-height: 100vh; }
  .header { background: #1a1a2e; padding: 16px 32px; display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid #2a2a3e; }
  .header h1 { font-size: 16px; font-weight: 500; color: #fff; }
  .header-right { display: flex; gap: 12px; align-items: center; }
  .nav-link { font-size: 13px; color: #888; text-decoration: none; padding: 6px 12px; border-radius: 6px; border: 1px solid #2a2a3e; }
  .nav-link:hover { color: #fff; border-color: #4f46e5; }
  .container { max-width: 800px; margin: 0 auto; padding: 32px; }
  .page-title { font-size: 20px; font-weight: 500; color: #fff; margin-bottom: 6px; }
  .page-sub { font-size: 13px; color: #666; margin-bottom: 32px; line-height: 1.6; }
  .article-ref { display: inline-block; background: #1a2a4e; color: #93c5fd; border-radius: 4px; padding: 2px 8px; font-size: 11px; margin-left: 6px; }
  .card { background: #1a1a2e; border: 1px solid #2a2a3e; border-radius: 12px; padding: 24px; margin-bottom: 20px; }
  .card-title { font-size: 15px; font-weight: 500; color: #fff; margin-bottom: 6px; }
  .card-sub { font-size: 13px; color: #666; margin-bottom: 20px; line-height: 1.5; }
  .stats-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; margin-bottom: 20px; }
  .stat { background: #0f0f1a; border: 1px solid #1e1e2e; border-radius: 8px; padding: 14px; text-align: center; }
  .stat-val { font-size: 22px; font-weight: 600; color: #e0e0e0; }
  .stat-label { font-size: 11px; color: #555; margin-top: 3px; text-transform: uppercase; letter-spacing: 0.05em; }
  .info-row { display: flex; justify-content: space-between; align-items: center; padding: 10px 0; border-bottom: 1px solid #1e1e2e; }
  .info-row:last-child { border-bottom: none; }
  .info-label { font-size: 13px; color: #666; }
  .info-val { font-size: 13px; color: #e0e0e0; font-family: monospace; }
  .form-row { display: flex; gap: 10px; align-items: flex-end; margin-top: 16px; }
  .form-group { flex: 1; }
  .form-label { font-size: 12px; color: #666; margin-bottom: 6px; display: block; }
  .form-input { width: 100%; background: #0f0f1a; border: 1px solid #2a2a3e; border-radius: 8px; padding: 10px 14px; font-size: 14px; color: #e0e0e0; outline: none; }
  .form-input:focus { border-color: #4f46e5; }
  .btn { padding: 10px 20px; border: none; border-radius: 8px; font-size: 13px; font-weight: 500; cursor: pointer; }
  .btn-primary { background: #4f46e5; color: #fff; }
  .btn-primary:hover { background: #4338ca; }
  .btn-primary:disabled { background: #2a2a3e; cursor: not-allowed; }
  .btn-warning { background: #7c2d12; color: #fff; border: 1px solid #9a3412; }
  .btn-warning:hover { background: #9a3412; }
  .btn-danger { background: #450a0a; color: #f87171; border: 1px solid #7f1d1d; }
  .btn-danger:hover { background: #7f1d1d; color: #fff; }
  .btn-danger:disabled { opacity: 0.4; cursor: not-allowed; }
  .result-box { background: #0f0f1a; border: 1px solid #2a2a3e; border-radius: 8px; padding: 14px; margin-top: 14px; font-size: 13px; color: #4ade80; line-height: 1.6; display: none; }
  .result-box.error { color: #f87171; }
  .result-box.show { display: block; }
  .warning-box { background: #1f1000; border: 1px solid #4a3000; border-radius: 8px; padding: 12px 14px; font-size: 12px; color: #fbbf24; line-height: 1.6; margin-bottom: 16px; }
  .gdpr-badge { display: inline-block; background: #1a2a4e; color: #93c5fd; border-radius: 4px; padding: 2px 8px; font-size: 11px; }
</style>
</head>
<body>
<div class="header">
  <h1>Data Retention & GDPR Erasure <span class="article-ref">EU AI Act Article 17</span></h1>
  <div class="header-right">
    <a href="/incidents" class="nav-link">Incidents</a>
    <a href="/dashboard" class="nav-link">Audit Log</a>
    <a href="/" class="nav-link">Chat UI</a>
  </div>
</div>

<div class="container">
  <p class="page-title">Data Retention Policy</p>
  <p class="page-sub">Configure how long audit logs, incidents, and review queue records are retained. A background process runs every 24 hours and automatically purges records older than the retention period. GDPR Article 17 right to erasure allows deletion of all data for a specific API key.</p>

  <!-- Current policy + storage stats -->
  <div class="card" id="policyCard">
    <p class="card-title">Current Policy</p>
    <p class="card-sub">Loading...</p>
  </div>

  <!-- Update retention days -->
  <div class="card">
    <p class="card-title">Update Retention Period</p>
    <p class="card-sub">Records older than this many days will be automatically deleted during the nightly purge. Minimum 1 day, maximum 3650 days (10 years). Unresolved incidents are always kept regardless of age.</p>
    <div class="form-row">
      <div class="form-group">
        <label class="form-label">Retention period (days)</label>
        <input class="form-input" type="number" id="retentionDays" min="1" max="3650" value="90" />
      </div>
      <div class="form-group" style="flex:0.5">
        <label class="form-label">Updated by</label>
        <input class="form-input" type="text" id="updatedBy" value="admin" />
      </div>
      <button class="btn btn-primary" onclick="updatePolicy()">Update policy</button>
    </div>
    <div class="result-box" id="updateResult"></div>
  </div>

  <!-- Manual purge -->
  <div class="card">
    <p class="card-title">Manual Purge</p>
    <p class="card-sub">Trigger an immediate purge of all records older than the current retention period. This is the same operation that runs automatically every 24 hours. Useful for testing or emergency data reduction.</p>
    <button class="btn btn-warning" onclick="triggerPurge()">Run purge now</button>
    <div class="result-box" id="purgeResult"></div>
  </div>

  <!-- GDPR Right to Erasure -->
  <div class="card">
    <p class="card-title">GDPR Right to Erasure <span class="gdpr-badge">Article 17</span></p>
    <p class="card-sub">Delete all audit log entries associated with a specific API key. This satisfies the GDPR "right to be forgotten" requirement. This action is permanent and cannot be undone.</p>
    <div class="warning-box">
      ⚠ This permanently deletes all audit log records for the specified API key. Incidents and review queue items are not affected. This action cannot be undone.
    </div>
    <div class="form-row">
      <div class="form-group">
        <label class="form-label">API Key to erase</label>
        <input class="form-input" type="text" id="eraseKey" placeholder="key-alpha-123" style="font-family:monospace" />
      </div>
      <button class="btn btn-danger" onclick="eraseData()" id="eraseBtn">Erase all data</button>
    </div>
    <div class="result-box" id="eraseResult"></div>
  </div>
</div>

<script>
  async function loadPolicy() {
    try {
      const res = await fetch('/admin/retention');
      const data = await res.json();
      if (!res.ok) return;

      const p = data.policy;
      const s = data.storage;
      const card = document.getElementById('policyCard');

      card.innerHTML =
        '<p class="card-title">Current Policy</p>' +
        '<div class="stats-grid">' +
          '<div class="stat"><div class="stat-val">' + (s.audit_logs || 0) + '</div><div class="stat-label">Audit logs</div></div>' +
          '<div class="stat"><div class="stat-val">' + (s.incidents || 0) + '</div><div class="stat-label">Incidents</div></div>' +
          '<div class="stat"><div class="stat-val">' + (s.review_queue || 0) + '</div><div class="stat-label">Review items</div></div>' +
        '</div>' +
        '<div class="info-row"><span class="info-label">Retention period</span><span class="info-val">' + p.retention_days + ' days</span></div>' +
        '<div class="info-row"><span class="info-label">Oldest log</span><span class="info-val">' + (s.oldest_log || '—') + '</span></div>' +
        '<div class="info-row"><span class="info-label">Newest log</span><span class="info-val">' + (s.newest_log || '—') + '</span></div>' +
        '<div class="info-row"><span class="info-label">Last updated</span><span class="info-val">' + p.updated_at + ' by ' + p.updated_by + '</span></div>';

      document.getElementById('retentionDays').value = p.retention_days;
    } catch (err) {
      console.error('Failed to load policy:', err);
    }
  }

  async function updatePolicy() {
    const days = parseInt(document.getElementById('retentionDays').value);
    const updatedBy = document.getElementById('updatedBy').value || 'admin';
    const el = document.getElementById('updateResult');

    try {
      const res = await fetch('/admin/retention', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ days, updated_by: updatedBy })
      });
      const data = await res.json();
      el.className = 'result-box show' + (res.ok ? '' : ' error');
      el.textContent = res.ok
        ? '✓ ' + data.message
        : '✗ Error: ' + data.reason;
      if (res.ok) loadPolicy();
    } catch (err) {
      el.className = 'result-box show error';
      el.textContent = '✗ Request failed: ' + err.message;
    }
  }

  async function triggerPurge() {
    const el = document.getElementById('purgeResult');
    el.className = 'result-box show';
    el.textContent = 'Running purge...';

    try {
      const res = await fetch('/admin/retention/purge', { method: 'POST' });
      const data = await res.json();
      el.className = 'result-box show' + (res.ok ? '' : ' error');
      if (res.ok) {
        const r = data.result;
        el.textContent = '✓ Purge complete — ' +
          r.audit_logs_deleted + ' audit logs, ' +
          r.incidents_deleted + ' incidents, ' +
          r.review_items_deleted + ' review items deleted (records before ' +
          r.purged_before.substring(0, 10) + ')';
        loadPolicy();
      } else {
        el.textContent = '✗ Error: ' + data.reason;
      }
    } catch (err) {
      el.className = 'result-box show error';
      el.textContent = '✗ Request failed: ' + err.message;
    }
  }

  async function eraseData() {
    const apiKey = document.getElementById('eraseKey').value.trim();
    const el = document.getElementById('eraseResult');
    const btn = document.getElementById('eraseBtn');

    if (!apiKey) {
      el.className = 'result-box show error';
      el.textContent = '✗ Please enter an API key';
      return;
    }

    if (!confirm('Are you sure you want to permanently delete all audit logs for key "' + apiKey + '"? This cannot be undone.')) {
      return;
    }

    btn.disabled = true;
    btn.textContent = 'Erasing...';
    el.className = 'result-box show';
    el.textContent = 'Processing erasure request...';

    try {
      const res = await fetch('/admin/retention/erase', {
        method: 'DELETE',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ api_key: apiKey })
      });
      const data = await res.json();
      el.className = 'result-box show' + (res.ok ? '' : ' error');
      el.textContent = res.ok ? '✓ ' + data.message : '✗ Error: ' + data.reason;
      if (res.ok) loadPolicy();
    } catch (err) {
      el.className = 'result-box show error';
      el.textContent = '✗ Request failed: ' + err.message;
    }

    btn.disabled = false;
    btn.textContent = 'Erase all data';
  }

  loadPolicy();
</script>
</body>
</html>`
