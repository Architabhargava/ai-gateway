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
	ID        int
	Timestamp string
	ClientIP  string
	Prompt    string
	Response  string
	Status    string
	Blocked   bool
	Reason    string
}

type Stats struct {
	Total   int
	Allowed int
	Blocked int
	Errors  int
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
				!strings.Contains(strings.ToLower(l.ClientIP), sl) {
				continue
			}
		}
		logs = append(logs, LogEntry{
			ID:        l.ID,
			Timestamp: l.Timestamp.Format("2006-01-02 15:04:05"),
			ClientIP:  l.ClientIP,
			Prompt:    l.Prompt,
			Response:  l.Response,
			Status:    l.Status,
			Blocked:   l.Blocked,
			Reason:    l.Reason,
		})
	}

	stats := d.getStats(logs)
	buckets := d.getHourlyBuckets(allLogs)

	data := struct {
		Logs      []LogEntry
		Stats     Stats
		Buckets   []HourBucket
		Timestamp string
		Search    string
	}{
		Logs:      logs,
		Stats:     stats,
		Buckets:   buckets,
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		Search:    search,
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
	var entries []LogEntry
	for _, l := range logs {
		entries = append(entries, LogEntry{Status: l.Status})
	}
	stats := d.getStats(entries)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (d *Dashboard) getStats(logs []LogEntry) Stats {
	stats := Stats{Total: len(logs)}
	for _, log := range logs {
		switch log.Status {
		case "allowed":
			stats.Allowed++
		case "blocked":
			stats.Blocked++
		case "error":
			stats.Errors++
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
	fmt.Fprintln(w, "404 - not found")
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
  .history-meta { font-size: 11px; color: #555; margin-top: 3px; }
  .badge-sm { display: inline-block; padding: 1px 7px; border-radius: 10px; font-size: 10px; font-weight: 500; }
  .badge-sm.allowed { background: #14532d; color: #4ade80; }
  .badge-sm.blocked { background: #450a0a; color: #f87171; }
  .badge-sm.error { background: #451a03; color: #fb923c; }
  .chat-area { flex: 1; display: flex; flex-direction: column; overflow: hidden; }
  .messages { flex: 1; overflow-y: auto; padding: 24px 32px; display: flex; flex-direction: column; gap: 20px; }
  .welcome { text-align: center; margin: auto; max-width: 480px; }
  .welcome h2 { font-size: 24px; font-weight: 500; color: #fff; margin-bottom: 10px; }
  .welcome p { font-size: 14px; color: #666; line-height: 1.7; margin-bottom: 24px; }
  .suggestion-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
  .suggestion { background: #1a1a2e; border: 1px solid #2a2a3e; border-radius: 10px; padding: 12px 14px; font-size: 13px; color: #aaa; cursor: pointer; text-align: left; }
  .suggestion:hover { border-color: #4f46e5; color: #fff; background: #1e1e40; }
  .msg { display: flex; flex-direction: column; gap: 4px; max-width: 780px; }
  .msg.user { align-self: flex-end; align-items: flex-end; }
  .msg.assistant { align-self: flex-start; align-items: flex-start; }
  .msg-bubble { padding: 12px 16px; border-radius: 12px; font-size: 14px; line-height: 1.7; }
  .msg.user .msg-bubble { background: #4f46e5; color: #fff; border-bottom-right-radius: 3px; }
  .msg.assistant .msg-bubble { background: #1a1a2e; color: #ddd; border-bottom-left-radius: 3px; border: 1px solid #2a2a3e; }
  .msg.blocked-msg .msg-bubble { background: #1f0a0a; border: 1px solid #450a0a; color: #f87171; }
  .msg-meta { font-size: 11px; color: #555; }
  .typing { display: flex; gap: 5px; align-items: center; padding: 14px 16px; background: #1a1a2e; border-radius: 12px; border: 1px solid #2a2a3e; border-bottom-left-radius: 3px; }
  .dot { width: 7px; height: 7px; border-radius: 50%; background: #4f46e5; animation: bounce 1.2s infinite; }
  .dot:nth-child(2) { animation-delay: 0.2s; }
  .dot:nth-child(3) { animation-delay: 0.4s; }
  @keyframes bounce { 0%,60%,100%{transform:translateY(0)} 30%{transform:translateY(-6px)} }
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
  <h1>AI Gateway</h1>
  <div class="topbar-right">
    <input class="key-input" id="apiKeyInput" type="password" placeholder="Paste your API key..." />
    <a href="/dashboard" class="nav-link">Dashboard</a>
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
        <h2>AI Gateway</h2>
        <p>Your prompts pass through the policy engine, get logged, and are sent to Groq AI. Enter your API key in the top right to get started.</p>
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
      <div class="hint">Press Enter to send &nbsp;·&nbsp; Shift+Enter for new line</div>
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
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendPromptToGateway();
    }
  });

  function useSuggestion(btn) {
    textarea.value = btn.textContent;
    textarea.focus();
  }

  function getApiKey() {
    return document.getElementById('apiKeyInput').value.trim();
  }

  function addMessage(role, text, status) {
    const welcome = document.getElementById('welcome');
    if (welcome) welcome.remove();

    const messages = document.getElementById('messages');
    const div = document.createElement('div');
    div.className = 'msg ' + role + (status === 'blocked' ? ' blocked-msg' : '');

    const bubble = document.createElement('div');
    bubble.className = 'msg-bubble';
    bubble.textContent = text;

    const meta = document.createElement('div');
    meta.className = 'msg-meta';
    meta.textContent = new Date().toLocaleTimeString();
    if (status) meta.textContent += ' · ' + status;

    div.appendChild(bubble);
    div.appendChild(meta);
    messages.appendChild(div);
    messages.scrollTop = messages.scrollHeight;
    return div;
  }

  function addTyping() {
    const welcome = document.getElementById('welcome');
    if (welcome) welcome.remove();
    const messages = document.getElementById('messages');
    const div = document.createElement('div');
    div.className = 'msg assistant';
    div.id = 'typing-indicator';
    div.innerHTML = '<div class="typing"><div class="dot"></div><div class="dot"></div><div class="dot"></div></div>';
    messages.appendChild(div);
    messages.scrollTop = messages.scrollHeight;
  }

  function removeTyping() {
    const t = document.getElementById('typing-indicator');
    if (t) t.remove();
  }

  function addToHistory(prompt, status) {
    history.unshift({ prompt, status });
    const list = document.getElementById('historyList');
    list.innerHTML = '';
    history.slice(0, 20).forEach((item, i) => {
      const div = document.createElement('div');
      div.className = 'history-item' + (i === 0 ? ' active' : '');
      div.innerHTML =
        '<div class="history-prompt">' + item.prompt + '</div>' +
        '<div class="history-meta"><span class="badge-sm ' + item.status + '">' + item.status + '</span></div>';
      div.onclick = () => {
        textarea.value = item.prompt;
        textarea.focus();
      };
      list.appendChild(div);
    });
  }

  async function sendPromptToGateway() {
    const prompt = textarea.value.trim();
    if (!prompt) return;

    const apiKey = getApiKey();
    if (!apiKey) {
      alert('Please paste your API key in the top right field first.\n\nUse one of: key-alpha-123, key-beta-456, key-gamma-789');
      return;
    }

    textarea.value = '';
    textarea.style.height = 'auto';
    document.getElementById('sendBtn').disabled = true;

    addMessage('user', prompt, null);
    addTyping();

    try {
      const res = await fetch('/ai', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-API-Key': apiKey
        },
        body: JSON.stringify({ prompt })
      });

      const data = await res.json();
      removeTyping();

      if (res.status === 401) {
        addMessage('assistant', 'Invalid or missing API key. Check the key in the top right.', 'error');
        addToHistory(prompt, 'error');
      } else if (res.status === 403) {
        addMessage('assistant', 'Blocked: ' + data.reason, 'blocked');
        addToHistory(prompt, 'blocked');
      } else if (res.ok) {
        addMessage('assistant', data.response, 'allowed');
        addToHistory(prompt, 'allowed');
      } else {
        addMessage('assistant', 'Error: ' + (data.error || 'Something went wrong'), 'error');
        addToHistory(prompt, 'error');
      }
    } catch (err) {
      removeTyping();
      addMessage('assistant', 'Could not reach the gateway. Is it running?', 'error');
      addToHistory(prompt, 'error');
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
<title>AI Gateway Dashboard</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #f5f5f5; color: #333; }
  .header { background: #1a1a2e; color: white; padding: 20px 32px; display: flex; justify-content: space-between; align-items: center; }
  .header h1 { font-size: 20px; font-weight: 500; }
  .header-right { display: flex; align-items: center; gap: 16px; }
  .header span { font-size: 13px; opacity: 0.6; }
  .auto-badge { font-size: 11px; background: #16a34a; color: white; padding: 3px 10px; border-radius: 20px; }
  .nav-link { font-size: 13px; color: #aaa; text-decoration: none; padding: 6px 14px; border: 1px solid #3a3a5e; border-radius: 6px; }
  .nav-link:hover { color: #fff; border-color: #6366f1; }
  .container { max-width: 1200px; margin: 0 auto; padding: 24px 32px; }
  .stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 16px; margin-bottom: 28px; }
  .stat-card { background: white; border-radius: 10px; padding: 20px; border: 1px solid #eee; }
  .stat-card .label { font-size: 12px; color: #888; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 8px; }
  .stat-card .value { font-size: 32px; font-weight: 600; }
  .stat-card.total .value { color: #1a1a2e; }
  .stat-card.allowed .value { color: #16a34a; }
  .stat-card.blocked .value { color: #dc2626; }
  .stat-card.errors .value { color: #d97706; }
  .chart-wrap { background: white; border-radius: 10px; border: 1px solid #eee; padding: 20px; margin-bottom: 28px; }
  .chart-title { font-size: 13px; font-weight: 500; color: #666; margin-bottom: 16px; }
  .chart { display: flex; align-items: flex-end; gap: 6px; height: 80px; }
  .bar-group { display: flex; flex-direction: column; align-items: center; flex: 1; gap: 4px; }
  .bar { width: 100%; background: #4f46e5; border-radius: 3px 3px 0 0; min-height: 2px; }
  .bar-label { font-size: 10px; color: #aaa; }
  .no-chart { color: #aaa; font-size: 13px; text-align: center; padding: 20px 0; }
  .toolbar { display: flex; align-items: center; justify-content: space-between; margin-bottom: 14px; }
  .search-wrap { display: flex; gap: 8px; }
  .search-wrap input { padding: 8px 14px; border: 1px solid #ddd; border-radius: 8px; font-size: 13px; width: 280px; outline: none; }
  .search-wrap input:focus { border-color: #4f46e5; }
  .search-wrap button { padding: 8px 16px; background: #4f46e5; color: white; border: none; border-radius: 8px; font-size: 13px; cursor: pointer; }
  .clear-btn { padding: 8px 14px; background: white; color: #666; border: 1px solid #ddd; border-radius: 8px; font-size: 13px; cursor: pointer; text-decoration: none; }
  .section-title { font-size: 15px; font-weight: 500; color: #444; }
  .table-wrap { background: white; border-radius: 10px; border: 1px solid #eee; overflow: hidden; }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  thead { background: #f9f9f9; }
  th { padding: 12px 16px; text-align: left; font-weight: 500; color: #666; font-size: 12px; text-transform: uppercase; letter-spacing: 0.04em; border-bottom: 1px solid #eee; }
  td { padding: 12px 16px; border-bottom: 1px solid #f0f0f0; vertical-align: top; max-width: 220px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  tr:last-child td { border-bottom: none; }
  tr:hover td { background: #fafafa; }
  .badge { display: inline-block; padding: 2px 10px; border-radius: 20px; font-size: 11px; font-weight: 500; }
  .badge.allowed { background: #dcfce7; color: #16a34a; }
  .badge.blocked { background: #fee2e2; color: #dc2626; }
  .badge.error { background: #fef3c7; color: #d97706; }
  .empty { text-align: center; padding: 48px; color: #aaa; font-size: 14px; }
</style>
</head>
<body>
<div class="header">
  <h1>AI Gateway — Audit Dashboard</h1>
  <div class="header-right">
    <span class="auto-badge">Auto-refresh ON</span>
    <span id="countdown">Refreshing in 10s</span>
    <a href="/" class="nav-link">Chat UI</a>
  </div>
</div>
<div class="container">
  <div class="stats">
    <div class="stat-card total"><div class="label">Total Requests</div><div class="value">{{.Stats.Total}}</div></div>
    <div class="stat-card allowed"><div class="label">Allowed</div><div class="value">{{.Stats.Allowed}}</div></div>
    <div class="stat-card blocked"><div class="label">Blocked</div><div class="value">{{.Stats.Blocked}}</div></div>
    <div class="stat-card errors"><div class="label">Errors</div><div class="value">{{.Stats.Errors}}</div></div>
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
    <p class="section-title">Recent requests (last 50)</p>
    <form class="search-wrap" method="GET" action="/dashboard">
      <input type="text" name="search" placeholder="Search prompts, status, IP..." value="{{.Search}}">
      <button type="submit">Search</button>
      {{if .Search}}<a href="/dashboard" class="clear-btn">Clear</a>{{end}}
    </form>
  </div>
  <div class="table-wrap">
    {{if .Logs}}
    <table>
      <thead>
        <tr><th>#</th><th>Time</th><th>Client IP</th><th>Prompt</th><th>Response</th><th>Status</th><th>Reason</th></tr>
      </thead>
      <tbody>
        {{range .Logs}}
        <tr>
          <td>{{.ID}}</td>
          <td>{{.Timestamp}}</td>
          <td>{{.ClientIP}}</td>
          <td title="{{.Prompt}}">{{.Prompt}}</td>
          <td title="{{.Response}}">{{.Response}}</td>
          <td><span class="badge {{.Status}}">{{.Status}}</span></td>
          <td>{{.Reason}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{else}}
    <div class="empty">{{if .Search}}No results for "{{.Search}}"{{else}}No requests yet.{{end}}</div>
    {{end}}
  </div>
</div>
<script>
  let s = 10;
  const el = document.getElementById('countdown');
  setInterval(() => {
    s--;
    if (s <= 0) {
      const search = new URLSearchParams(window.location.search).get('search') || '';
      window.location.href = '/dashboard' + (search ? '?search=' + encodeURIComponent(search) : '');
    }
    el.textContent = 'Refreshing in ' + s + 's';
  }, 1000);
</script>
</body>
</html>`
