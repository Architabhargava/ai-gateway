package dashboard

import "net/http"

// HandleMetricsPage serves the metrics/observability panel at /metrics-dashboard
func (d *Dashboard) HandleMetricsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(metricsPageHTML))
}

const metricsPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AI Gateway — Metrics</title>
<script src="https://cdnjs.cloudflare.com/ajax/libs/Chart.js/4.4.1/chart.umd.min.js"></script>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#0a0a12;color:#e0e0e0;min-height:100vh}
.header{background:#111119;padding:16px 32px;display:flex;justify-content:space-between;align-items:center;border-bottom:1px solid #1e1e2e}
.header h1{font-size:16px;font-weight:500;color:#fff}
.header-right{display:flex;gap:12px;align-items:center}
.nav-link{font-size:13px;color:#888;text-decoration:none;padding:6px 12px;border-radius:6px;border:1px solid #2a2a3e}
.nav-link:hover{color:#fff;border-color:#4f46e5}
.pulse{width:8px;height:8px;border-radius:50%;background:#4ade80;animation:pulse 2s infinite}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:0.3}}
.live-label{font-size:12px;color:#4ade80;display:flex;align-items:center;gap:6px}
.container{max-width:1200px;margin:0 auto;padding:28px 32px}
.page-title{font-size:20px;font-weight:500;color:#fff;margin-bottom:4px}
.page-sub{font-size:13px;color:#555;margin-bottom:28px}

/* KPI row */
.kpi-row{display:grid;grid-template-columns:repeat(6,1fr);gap:12px;margin-bottom:24px}
.kpi{background:#111119;border:1px solid #1e1e2e;border-radius:10px;padding:16px;text-align:center}
.kpi-val{font-size:28px;font-weight:600;margin-bottom:4px}
.kpi-lbl{font-size:11px;color:#555;text-transform:uppercase;letter-spacing:0.05em}
.kpi.total .kpi-val{color:#e0e0e0}
.kpi.allowed .kpi-val{color:#4ade80}
.kpi.blocked .kpi-val{color:#f87171}
.kpi.rate .kpi-val{color:#fbbf24}
.kpi.high .kpi-val{color:#f87171}
.kpi.score .kpi-val{color:#a5b4fc}

/* Chart grid */
.chart-grid{display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-bottom:16px}
.chart-grid.wide{grid-template-columns:2fr 1fr}
.chart-card{background:#111119;border:1px solid #1e1e2e;border-radius:10px;padding:20px}
.chart-title{font-size:12px;font-weight:500;color:#888;text-transform:uppercase;letter-spacing:0.06em;margin-bottom:16px;display:flex;justify-content:space-between;align-items:center}
.chart-title span{font-size:10px;color:#444;font-weight:400;text-transform:none;letter-spacing:0}
.chart-wrap{position:relative;height:200px}
.chart-wrap.tall{height:240px}
.empty-chart{display:flex;align-items:center;justify-content:center;height:100%;color:#333;font-size:13px}

/* Last updated */
.footer{text-align:center;font-size:11px;color:#333;margin-top:24px;padding-bottom:32px}
</style>
</head>
<body>
<div class="header">
  <h1>Observability — Metrics Dashboard</h1>
  <div class="header-right">
    <div class="live-label"><div class="pulse"></div>Live — refreshes every 10s</div>
    <a href="/platform" class="nav-link">← Platform</a>
  </div>
</div>

<div class="container">
  <p class="page-title">Gateway Metrics</p>
  <p class="page-sub">Real-time aggregated metrics from the audit log. All data is computed from your SQLite database — no external service required.</p>

  <!-- KPI row -->
  <div class="kpi-row">
    <div class="kpi total"><div class="kpi-val" id="kTotal">—</div><div class="kpi-lbl">Total requests</div></div>
    <div class="kpi allowed"><div class="kpi-val" id="kAllowed">—</div><div class="kpi-lbl">Allowed</div></div>
    <div class="kpi blocked"><div class="kpi-val" id="kBlocked">—</div><div class="kpi-lbl">Blocked</div></div>
    <div class="kpi rate"><div class="kpi-val" id="kRate">—%</div><div class="kpi-lbl">Block rate</div></div>
    <div class="kpi high"><div class="kpi-val" id="kHigh">—</div><div class="kpi-lbl">High risk</div></div>
    <div class="kpi score"><div class="kpi-val" id="kScore">—</div><div class="kpi-lbl">Avg classifier score</div></div>
  </div>

  <!-- Row 1: Hourly traffic + Risk breakdown -->
  <div class="chart-grid wide">
    <div class="chart-card">
      <div class="chart-title">Requests by hour <span>last 24 hours</span></div>
      <div class="chart-wrap tall"><canvas id="chartHourly"></canvas></div>
    </div>
    <div class="chart-card">
      <div class="chart-title">Risk level breakdown <span>all time</span></div>
      <div class="chart-wrap tall"><canvas id="chartRisk"></canvas></div>
    </div>
  </div>

  <!-- Row 2: Daily trend + Blocked by category -->
  <div class="chart-grid">
    <div class="chart-card">
      <div class="chart-title">Daily request trend <span>last 7 days</span></div>
      <div class="chart-wrap"><canvas id="chartDaily"></canvas></div>
    </div>
    <div class="chart-card">
      <div class="chart-title">Blocked by category <span>all time</span></div>
      <div class="chart-wrap"><canvas id="chartCategory"></canvas></div>
    </div>
  </div>

  <!-- Row 3: Classifier score distribution -->
  <div class="chart-grid">
    <div class="chart-card">
      <div class="chart-title">Classifier score distribution <span>all requests</span></div>
      <div class="chart-wrap"><canvas id="chartScoreDist"></canvas></div>
    </div>
    <div class="chart-card">
      <div class="chart-title">Block rate over 7 days <span>blocked / total %</span></div>
      <div class="chart-wrap"><canvas id="chartBlockRate"></canvas></div>
    </div>
  </div>

  <div class="footer" id="lastUpdated">Loading...</div>
</div>

<script>
// Chart.js global defaults for dark theme
Chart.defaults.color = '#666';
Chart.defaults.borderColor = '#1e1e2e';
Chart.defaults.font.family = "-apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif";
Chart.defaults.font.size = 11;

const COLORS = {
  green:  '#4ade80',
  red:    '#f87171',
  amber:  '#fbbf24',
  blue:   '#60a5fa',
  purple: '#a78bfa',
  pink:   '#f472b6',
  teal:   '#2dd4bf',
  indigo: '#818cf8',
};

let charts = {};

function initCharts() {
  // ── Hourly traffic (stacked bar) ────────────────────────────────────────
  charts.hourly = new Chart(document.getElementById('chartHourly'), {
    type: 'bar',
    data: {
      labels: [],
      datasets: [
        { label: 'Allowed', data: [], backgroundColor: '#16a34a88', borderColor: '#4ade80', borderWidth: 1 },
        { label: 'Blocked', data: [], backgroundColor: '#dc262688', borderColor: '#f87171', borderWidth: 1 },
      ]
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      scales: {
        x: { stacked: true, grid: { color: '#1e1e2e' }, ticks: { maxTicksLimit: 8 } },
        y: { stacked: true, grid: { color: '#1e1e2e' }, beginAtZero: true }
      },
      plugins: { legend: { position: 'top', labels: { boxWidth: 12, padding: 16 } } }
    }
  });

  // ── Risk level doughnut ─────────────────────────────────────────────────
  charts.risk = new Chart(document.getElementById('chartRisk'), {
    type: 'doughnut',
    data: {
      labels: ['Minimal', 'Limited', 'High', 'Unacceptable'],
      datasets: [{
        data: [0, 0, 0, 0],
        backgroundColor: ['#16a34a', '#d97706', '#dc2626', '#9333ea'],
        borderColor: '#111119',
        borderWidth: 3,
      }]
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      cutout: '65%',
      plugins: {
        legend: { position: 'bottom', labels: { boxWidth: 12, padding: 12 } }
      }
    }
  });

  // ── Daily trend (line) ──────────────────────────────────────────────────
  charts.daily = new Chart(document.getElementById('chartDaily'), {
    type: 'line',
    data: {
      labels: [],
      datasets: [
        {
          label: 'Total', data: [], borderColor: COLORS.indigo,
          backgroundColor: '#818cf822', fill: true, tension: 0.4, pointRadius: 4,
        },
        {
          label: 'Blocked', data: [], borderColor: COLORS.red,
          backgroundColor: 'transparent', tension: 0.4, pointRadius: 4,
        },
      ]
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      scales: {
        x: { grid: { color: '#1e1e2e' } },
        y: { grid: { color: '#1e1e2e' }, beginAtZero: true }
      },
      plugins: { legend: { position: 'top', labels: { boxWidth: 12, padding: 16 } } }
    }
  });

  // ── Blocked by category (horizontal bar) ───────────────────────────────
  charts.category = new Chart(document.getElementById('chartCategory'), {
    type: 'bar',
    data: {
      labels: [],
      datasets: [{
        label: 'Blocked requests',
        data: [],
        backgroundColor: [
          '#f87171', '#fbbf24', '#a78bfa', '#60a5fa',
          '#f472b6', '#2dd4bf', '#818cf8', '#fb923c'
        ],
        borderWidth: 0,
      }]
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      indexAxis: 'y',
      scales: {
        x: { grid: { color: '#1e1e2e' }, beginAtZero: true },
        y: { grid: { display: false } }
      },
      plugins: { legend: { display: false } }
    }
  });

  // ── Score distribution (bar) ────────────────────────────────────────────
  charts.scoreDist = new Chart(document.getElementById('chartScoreDist'), {
    type: 'bar',
    data: {
      labels: ['0.0–0.2', '0.2–0.4', '0.4–0.6', '0.6–0.8', '0.8–1.0'],
      datasets: [{
        label: 'Requests',
        data: [0, 0, 0, 0, 0],
        backgroundColor: ['#4ade80', '#86efac', '#fbbf24', '#f97316', '#f87171'],
        borderWidth: 0,
        borderRadius: 4,
      }]
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      scales: {
        x: { grid: { display: false } },
        y: { grid: { color: '#1e1e2e' }, beginAtZero: true }
      },
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            title: (items) => 'Score range ' + items[0].label,
            label: (item) => item.raw + ' requests',
          }
        }
      }
    }
  });

  // ── Block rate % (line) ─────────────────────────────────────────────────
  charts.blockRate = new Chart(document.getElementById('chartBlockRate'), {
    type: 'line',
    data: {
      labels: [],
      datasets: [{
        label: 'Block rate %',
        data: [],
        borderColor: COLORS.amber,
        backgroundColor: '#fbbf2422',
        fill: true,
        tension: 0.4,
        pointRadius: 4,
      }]
    },
    options: {
      responsive: true, maintainAspectRatio: false,
      scales: {
        x: { grid: { color: '#1e1e2e' } },
        y: {
          grid: { color: '#1e1e2e' }, beginAtZero: true, max: 100,
          ticks: { callback: (v) => v + '%' }
        }
      },
      plugins: { legend: { display: false } }
    }
  });
}

function updateCharts(data) {
  // KPIs
  document.getElementById('kTotal').textContent = data.summary.total || 0;
  document.getElementById('kAllowed').textContent = data.summary.allowed || 0;
  document.getElementById('kBlocked').textContent = data.summary.blocked || 0;
  document.getElementById('kRate').textContent = (data.block_rate || '0.0') + '%';
  document.getElementById('kHigh').textContent = (data.summary.high_risk || 0) + (data.summary.unacceptable || 0);
  document.getElementById('kScore').textContent = data.avg_score || '0.00';

  // Hourly chart
  const hourly = data.hourly || [];
  charts.hourly.data.labels = hourly.map(h => h.hour);
  charts.hourly.data.datasets[0].data = hourly.map(h => h.allowed);
  charts.hourly.data.datasets[1].data = hourly.map(h => h.blocked);
  charts.hourly.update('none');

  // Risk doughnut
  const risk = data.by_risk || {};
  charts.risk.data.datasets[0].data = [
    risk.minimal || 0, risk.limited || 0,
    risk.high || 0, risk.unacceptable || 0
  ];
  charts.risk.update('none');

  // Daily trend
  const daily = data.daily || [];
  charts.daily.data.labels = daily.map(d => d.day);
  charts.daily.data.datasets[0].data = daily.map(d => d.total);
  charts.daily.data.datasets[1].data = daily.map(d => d.blocked);
  charts.daily.update('none');

  // Block rate over 7 days
  charts.blockRate.data.labels = daily.map(d => d.day);
  charts.blockRate.data.datasets[0].data = daily.map(d =>
    d.total > 0 ? parseFloat((d.blocked / d.total * 100).toFixed(1)) : 0
  );
  charts.blockRate.update('none');

  // Category chart
  const cat = data.by_category || {};
  const catEntries = Object.entries(cat).sort((a, b) => b[1] - a[1]);
  if (catEntries.length > 0) {
    charts.category.data.labels = catEntries.map(([k]) => k.replace(/_/g, ' '));
    charts.category.data.datasets[0].data = catEntries.map(([, v]) => v);
    charts.category.update('none');
  }

  // Score distribution
  const dist = data.score_dist || {};
  charts.scoreDist.data.datasets[0].data = [
    dist['0.0-0.2'] || 0, dist['0.2-0.4'] || 0,
    dist['0.4-0.6'] || 0, dist['0.6-0.8'] || 0,
    dist['0.8-1.0'] || 0,
  ];
  charts.scoreDist.update('none');

  document.getElementById('lastUpdated').textContent =
    'Last updated: ' + data.generated_at + ' · Refreshes every 10s';
}

async function refresh() {
  try {
    const res = await fetch('/admin/metrics-data');
    if (!res.ok) return;
    const data = await res.json();
    updateCharts(data);
  } catch (e) {
    console.error('Metrics refresh error:', e);
  }
}

// Initialise charts then start polling
initCharts();
refresh();
setInterval(refresh, 10000);
</script>
</body>
</html>`
