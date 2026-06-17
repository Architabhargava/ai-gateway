package dashboard

import "net/http"

// HandleChat serves the public user-facing chat interface.
// Users authenticate with their gateway API key (gw_...).
// No admin features are exposed here — audit logs, incidents,
// key management, and retention are admin-only.
func (d *Dashboard) HandleChat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(chatHTML))
}

const chatHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AI Gateway</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#0a0a12;color:#e0e0e0;height:100vh;display:flex;flex-direction:column}
.topbar{background:#111119;padding:14px 24px;display:flex;justify-content:space-between;align-items:center;border-bottom:1px solid #1e1e2e;flex-shrink:0}
.topbar-left{display:flex;align-items:center;gap:12px}
.topbar-logo{font-size:15px;font-weight:500;color:#fff}
.eu-badge{display:inline-block;background:#1a2a4e;color:#93c5fd;border-radius:4px;padding:2px 8px;font-size:10px}
.topbar-right{display:flex;gap:10px;align-items:center}
.key-wrap{position:relative;display:flex;align-items:center}
.key-input{background:#1a1a28;border:1px solid #2a2a3e;border-radius:6px;padding:7px 36px 7px 12px;font-size:12px;color:#ccc;width:220px;outline:none;font-family:monospace}
.key-input:focus{border-color:#6366f1}
.key-toggle{position:absolute;right:8px;background:none;border:none;color:#555;cursor:pointer;font-size:13px;padding:0}
.key-toggle:hover{color:#aaa}
.key-status{font-size:11px;padding:3px 10px;border-radius:20px}
.key-status.ok{background:#14532d;color:#4ade80}
.key-status.err{background:#450a0a;color:#f87171}
.key-status.idle{background:#1e1e28;color:#666}
.layout{display:flex;flex:1;overflow:hidden}
.sidebar{width:240px;background:#0d0d18;border-right:1px solid #1e1e2e;display:flex;flex-direction:column;flex-shrink:0}
.sidebar-header{padding:14px 14px 10px;font-size:10px;text-transform:uppercase;letter-spacing:0.08em;color:#444;border-bottom:1px solid #1e1e2e}
.history-list{flex:1;overflow-y:auto;padding:8px}
.history-item{padding:8px 10px;border-radius:6px;cursor:pointer;margin-bottom:3px;border:1px solid transparent}
.history-item:hover{background:#1a1a28;border-color:#2a2a3e}
.history-item.active{background:#1e1e40;border-color:#4f46e5}
.hi-prompt{font-size:12px;color:#bbb;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.hi-meta{font-size:10px;color:#444;margin-top:2px;display:flex;gap:5px;align-items:center}
.bsm{display:inline-block;padding:1px 6px;border-radius:8px;font-size:10px;font-weight:500}
.bsm.allowed{background:#14532d;color:#4ade80}
.bsm.blocked{background:#450a0a;color:#f87171}
.bsm.error{background:#3a2a00;color:#fbbf24}
.bsm.review_pending{background:#2a1a00;color:#fb923c}
.rsm{display:inline-block;padding:1px 5px;border-radius:8px;font-size:10px}
.rsm.minimal{background:#0a2a1a;color:#4ade80}
.rsm.limited{background:#2a2000;color:#fbbf24}
.rsm.high{background:#2a0a0a;color:#f87171}
.rsm.unacceptable{background:#2a0a18;color:#f472b6}
.new-chat-btn{margin:10px;padding:8px;background:#1a1a28;border:1px solid #2a2a3e;border-radius:8px;color:#888;font-size:12px;cursor:pointer;text-align:center}
.new-chat-btn:hover{border-color:#4f46e5;color:#fff}
.chat-main{flex:1;display:flex;flex-direction:column;overflow:hidden}
.messages{flex:1;overflow-y:auto;padding:20px 28px;display:flex;flex-direction:column;gap:18px}
.welcome{text-align:center;margin:auto;max-width:440px}
.welcome h2{font-size:22px;font-weight:500;color:#fff;margin-bottom:8px}
.welcome p{font-size:13px;color:#555;line-height:1.7;margin-bottom:22px}
.suggestion-grid{display:grid;grid-template-columns:1fr 1fr;gap:8px;margin-bottom:16px}
.suggestion{background:#111119;border:1px solid #1e1e2e;border-radius:8px;padding:10px 12px;font-size:12px;color:#777;cursor:pointer;text-align:left;transition:all 0.15s}
.suggestion:hover{border-color:#4f46e5;color:#fff;background:#1e1e40}
.key-notice{background:#1a1a28;border:1px solid #2a2a3e;border-radius:8px;padding:12px 14px;font-size:12px;color:#555;text-align:center}
.key-notice span{color:#a5b4fc;font-family:monospace}
.msg{display:flex;flex-direction:column;gap:5px;max-width:720px}
.msg.user{align-self:flex-end;align-items:flex-end}
.msg.assistant{align-self:flex-start;align-items:flex-start}
.bubble{padding:10px 14px;border-radius:10px;font-size:14px;line-height:1.7;white-space:pre-wrap;word-break:break-word}
.msg.user .bubble{background:#4f46e5;color:#fff;border-bottom-right-radius:3px}
.msg.assistant .bubble{background:#1a1a28;color:#ddd;border-bottom-left-radius:3px;border:1px solid #2a2a3e}
.msg.blocked-msg .bubble{background:#1f0a0a;border:1px solid #450a0a;color:#f87171}
.msg-meta{font-size:10px;color:#444;display:flex;gap:6px;align-items:center;flex-wrap:wrap}
.reasoning-btn{font-size:11px;color:#4f46e5;background:none;border:none;padding:0;cursor:pointer}
.reasoning-content{background:#0a0a14;border:1px solid #1e1e2e;border-radius:6px;padding:8px 12px;font-size:11px;color:#666;line-height:1.6;margin-top:4px;display:none;max-width:680px;white-space:pre-wrap}
.reasoning-content.open{display:block}
.typing{display:flex;gap:4px;align-items:center;padding:10px 14px;background:#1a1a28;border-radius:10px;border:1px solid #2a2a3e;border-bottom-left-radius:3px}
.dot{width:6px;height:6px;border-radius:50%;background:#4f46e5;animation:bounce 1.2s infinite}
.dot:nth-child(2){animation-delay:.2s}.dot:nth-child(3){animation-delay:.4s}
@keyframes bounce{0%,60%,100%{transform:translateY(0)}30%{transform:translateY(-5px)}}
.review-wait{display:flex;gap:8px;align-items:center;padding:10px 14px;background:#1a1500;border-radius:10px;border:1px solid #4a3800;border-bottom-left-radius:3px;font-size:12px;color:#fbbf24}
.spinner{width:14px;height:14px;border:2px solid #4a3800;border-top-color:#fbbf24;border-radius:50%;animation:spin 1s linear infinite;flex-shrink:0}
@keyframes spin{to{transform:rotate(360deg)}}
.input-area{padding:12px 28px 20px;border-top:1px solid #1e1e2e;background:#0a0a12;flex-shrink:0}
.input-row{display:flex;gap:8px;align-items:flex-end;background:#1a1a28;border:1px solid #2a2a3e;border-radius:12px;padding:8px 12px}
.input-row:focus-within{border-color:#4f46e5}
textarea{flex:1;background:transparent;border:none;outline:none;color:#e0e0e0;font-size:14px;resize:none;min-height:22px;max-height:120px;font-family:inherit;line-height:1.5}
textarea::placeholder{color:#3a3a4e}
.send-btn{width:34px;height:34px;background:#4f46e5;border:none;border-radius:7px;cursor:pointer;display:flex;align-items:center;justify-content:center;flex-shrink:0}
.send-btn:hover{background:#4338ca}.send-btn:disabled{background:#2a2a3e;cursor:not-allowed}
.send-btn svg{width:14px;height:14px;fill:white}
.input-hint{font-size:10px;color:#2a2a3e;text-align:center;margin-top:6px}
.disclosure{font-size:10px;color:#2a2a3e;text-align:center;margin-top:3px}
</style>
</head>
<body>
<div class="topbar">
  <div class="topbar-left">
    <span class="topbar-logo">AI Gateway</span>
    <span class="eu-badge">EU AI Act Compliant</span>
  </div>
  <div class="topbar-right">
    <span class="key-status idle" id="keyStatus">No key</span>
    <div class="key-wrap">
      <input class="key-input" id="apiKey" type="password" placeholder="Paste your API key (gw_...)" oninput="onKeyInput()"/>
      <button class="key-toggle" onclick="toggleKeyVis()" id="keyToggleBtn" title="Show/hide key">👁</button>
    </div>
  </div>
</div>

<div class="layout">
  <div class="sidebar">
    <div class="sidebar-header">Conversation history</div>
    <div class="history-list" id="historyList"></div>
    <button class="new-chat-btn" onclick="newChat()">+ New conversation</button>
  </div>

  <div class="chat-main">
    <div class="messages" id="messages">
      <div class="welcome" id="welcome">
        <h2>AI Gateway</h2>
        <p>Your prompts are secured by multi-layer AI governance — authenticated, classified for safety, and logged for compliance. Paste your API key to begin.</p>
        <div class="suggestion-grid">
          <button class="suggestion" onclick="useSug(this)">What is machine learning?</button>
          <button class="suggestion" onclick="useSug(this)">Explain Docker in simple terms</button>
          <button class="suggestion" onclick="useSug(this)">How does a REST API work?</button>
          <button class="suggestion" onclick="useSug(this)">What is rate limiting?</button>
        </div>
        <div class="key-notice" id="keyNotice">
          Enter your API key above to get started.<br>Keys look like <span>gw_a1b2c3d4...</span>
        </div>
      </div>
    </div>

    <div class="input-area">
      <div class="input-row">
        <textarea id="prompt" placeholder="Ask anything..." rows="1"></textarea>
        <button class="send-btn" id="sendBtn" onclick="sendMsg()" title="Send" disabled>
          <svg viewBox="0 0 24 24"><path d="M2 21L23 12 2 3v7l15 2-15 2z"/></svg>
        </button>
      </div>
      <div class="input-hint">Enter to send &nbsp;·&nbsp; Shift+Enter for new line</div>
      <div class="disclosure">Responses are AI-generated and governed by the EU AI Act &nbsp;·&nbsp; All requests are logged for compliance</div>
    </div>
  </div>
</div>

<script>
// ── State ─────────────────────────────────────────────────────────────────
let chatHistory = [];
let keyVisible = false;

// ── Key input handling ────────────────────────────────────────────────────
function onKeyInput() {
  const key = document.getElementById('apiKey').value.trim();
  const status = document.getElementById('keyStatus');
  const btn = document.getElementById('sendBtn');
  const notice = document.getElementById('keyNotice');

  if (!key) {
    status.className = 'key-status idle'; status.textContent = 'No key';
    btn.disabled = true;
    if (notice) notice.style.display = '';
    return;
  }

  if (key.startsWith('gw_') && key.length > 10) {
    status.className = 'key-status ok'; status.textContent = 'Key ready';
    btn.disabled = false;
    if (notice) notice.style.display = 'none';
  } else {
    status.className = 'key-status err'; status.textContent = 'Invalid format';
    btn.disabled = true;
  }
}

function toggleKeyVis() {
  keyVisible = !keyVisible;
  const inp = document.getElementById('apiKey');
  inp.type = keyVisible ? 'text' : 'password';
  document.getElementById('keyToggleBtn').textContent = keyVisible ? '🙈' : '👁';
}

// ── Chat ──────────────────────────────────────────────────────────────────
const ta = document.getElementById('prompt');
ta.addEventListener('input', () => { ta.style.height='auto'; ta.style.height=Math.min(ta.scrollHeight,120)+'px'; });
ta.addEventListener('keydown', e => { if(e.key==='Enter'&&!e.shiftKey){e.preventDefault();sendMsg();} });
function useSug(btn) { ta.value = btn.textContent; ta.focus(); onKeyInput(); }
function getKey() { return document.getElementById('apiKey').value.trim(); }

function removeWelcome() { const w=document.getElementById('welcome'); if(w)w.remove(); }

function esc(s) { return (s||'').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }

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
    if (meta.status) m.innerHTML += ' · <span class="bsm '+meta.status+'">'+meta.status+'</span>';
    if (meta.risk_level) m.innerHTML += ' · <span class="rsm '+meta.risk_level+'">'+meta.risk_level+' risk</span>';
    if (meta.eu_article) m.innerHTML += ' · <span style="font-size:10px;color:#93c5fd">'+esc(meta.eu_article)+'</span>';
    div.appendChild(m);

    if (meta.reasoning_chain) {
      const btn = document.createElement('button');
      btn.className = 'reasoning-btn';
      btn.textContent = 'Why was this blocked? ↓';
      const box = document.createElement('div');
      box.className = 'reasoning-content';
      box.textContent = meta.reasoning_chain;
      btn.onclick = () => {
        box.classList.toggle('open');
        btn.textContent = box.classList.contains('open') ? 'Hide reasoning ↑' : 'Why was this blocked? ↓';
      };
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
    div.innerHTML = '<div class="review-wait"><div class="spinner"></div>Your request is under review — please wait...</div>';
  } else {
    div.innerHTML = '<div class="typing"><div class="dot"></div><div class="dot"></div><div class="dot"></div></div>';
  }
  msgs.appendChild(div); msgs.scrollTop = msgs.scrollHeight;
}

function removeTyping() { const t=document.getElementById('typing-indicator'); if(t)t.remove(); }

function addToHistory(prompt, status, risk) {
  chatHistory.unshift({ prompt, status, risk });
  renderHistory();
}

function renderHistory() {
  const list = document.getElementById('historyList');
  list.innerHTML = '';
  chatHistory.slice(0,30).forEach((item, i) => {
    const div = document.createElement('div');
    div.className = 'history-item' + (i===0?' active':'');
    div.innerHTML =
      '<div class="hi-prompt">' + esc(item.prompt) + '</div>' +
      '<div class="hi-meta">' +
        '<span class="bsm '+item.status+'">'+item.status+'</span>' +
        (item.risk?'<span class="rsm '+item.risk+'">'+item.risk+'</span>':'') +
      '</div>';
    div.onclick = () => { ta.value = item.prompt; ta.focus(); };
    list.appendChild(div);
  });
}

function newChat() {
  document.getElementById('messages').innerHTML =
    '<div class="welcome" id="welcome">' +
    '<h2>AI Gateway</h2>' +
    '<p>Your prompts are secured by multi-layer AI governance.</p>' +
    '<div class="suggestion-grid">' +
    '<button class="suggestion" onclick="useSug(this)">What is machine learning?</button>' +
    '<button class="suggestion" onclick="useSug(this)">Explain Docker in simple terms</button>' +
    '<button class="suggestion" onclick="useSug(this)">How does a REST API work?</button>' +
    '<button class="suggestion" onclick="useSug(this)">What is rate limiting?</button>' +
    '</div></div>';
}

async function sendMsg() {
  const prompt = ta.value.trim();
  if (!prompt) return;
  const key = getKey();
  if (!key) { alert('Please enter your API key first.'); return; }

  ta.value = ''; ta.style.height = 'auto';
  document.getElementById('sendBtn').disabled = true;
  addMsg('user', prompt, null);

  const likelySensitive = ['repeat','instructions','system','told','pretend','inject','context'].some(w=>prompt.toLowerCase().includes(w));
  addTyping(likelySensitive);

  try {
    const res = await fetch('/ai', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-API-Key': key },
      body: JSON.stringify({ prompt })
    });
    const data = await res.json();
    removeTyping();

    if (res.status === 401) {
      addMsg('assistant', 'Access denied: ' + (data.reason || 'invalid or revoked API key'), { status: 'error' });
      addToHistory(prompt, 'error', null);
    } else if (res.status === 429) {
      addMsg('assistant', 'Rate limit reached. ' + (data.reason || 'Please wait a moment and try again.'), { status: 'error' });
      addToHistory(prompt, 'error', null);
    } else if (res.status === 451) {
      addMsg('assistant',
        'This request cannot be processed — EU AI Act Article 5 violation.\n\n' + data.reason,
        { status: 'blocked', risk_level: 'unacceptable', eu_article: data.article }
      );
      addToHistory(prompt, 'blocked', 'unacceptable');
    } else if (res.status === 403) {
      addMsg('assistant',
        'Request blocked by safety policy.\n\n' + data.reason,
        { status: 'blocked', risk_level: data.risk_level, eu_article: data.eu_article, reasoning_chain: data.reasoning_chain }
      );
      addToHistory(prompt, 'blocked', data.risk_level);
    } else if (res.ok) {
      addMsg('assistant', data.response, { status: 'allowed', risk_level: data.risk_level });
      addToHistory(prompt, 'allowed', data.risk_level);
    } else {
      addMsg('assistant', 'Something went wrong: ' + (data.reason || 'unknown error'), { status: 'error' });
      addToHistory(prompt, 'error', null);
    }
  } catch (err) {
    removeTyping();
    addMsg('assistant', 'Could not reach the gateway. Please check your connection.', { status: 'error' });
    addToHistory(prompt, 'error', null);
  }

  document.getElementById('sendBtn').disabled = false;
  ta.focus();
}
</script>
</body>
</html>`
