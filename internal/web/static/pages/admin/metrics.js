const API_URL = '';

async function loadMetrics(days) {
  return fetchJson(`${API_URL}/v1/admin/metrics?days=${days}`);
}

async function loadFetchLatency(days) {
  return fetchJson(`${API_URL}/v1/admin/metrics/fetch-latency?days=${days}`);
}

async function loadGuestMetrics(days) {
  return fetchJson(`${API_URL}/v1/admin/metrics/guest?days=${days}`);
}

function escapeHtml(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

function renderMetrics(data) {
  const body = document.getElementById('metrics-body');
  if (!body) return;
  body.innerHTML = data.daily
    .map(
      (m) => `
        <tr class="border-b border-slate-100">
          <td class="px-4 py-3 text-slate-600">${escapeHtml(m.date)}</td>
          <td class="px-4 py-3 text-slate-800">${escapeHtml(m.event_name)}</td>
          <td class="px-4 py-3 text-slate-800">${m.called}</td>
          <td class="px-4 py-3 text-slate-800">${m.confirmed}</td>
          <td class="px-4 py-3 text-slate-600">${m.unconfirmed}</td>
          <td class="px-4 py-3 text-slate-600">${m.avg_confirm_minutes}</td>
        </tr>`,
    )
    .join('');
  if (data.daily.length === 0) {
    body.innerHTML = '<tr><td colspan="6" class="px-4 py-8 text-center text-slate-500">No data yet.</td></tr>';
  }
}

function renderGuestMetrics(data) {
  const body = document.getElementById('guest-body');
  if (!body) return;
  body.innerHTML = data.rows
    .map(
      (m) => `
        <tr class="border-b border-slate-100">
          <td class="px-4 py-3 text-slate-600">${escapeHtml(m.date)}</td>
          <td class="px-4 py-3 text-slate-800">${m.submissions}</td>
          <td class="px-4 py-3 text-slate-800">${m.children}</td>
          <td class="px-4 py-3 text-slate-800">${m.entered}</td>
          <td class="px-4 py-3 text-slate-800">${m.approved}</td>
          <td class="px-4 py-3 text-slate-600">${m.rejected}</td>
          <td class="px-4 py-3 text-slate-600">${m.pending}</td>
        </tr>`,
    )
    .join('');
  if (data.rows.length === 0) {
    body.innerHTML = '<tr><td colspan="7" class="px-4 py-8 text-center text-slate-500">No data yet.</td></tr>';
  }
}

function formatMs(value) {
  if (value === null || value === undefined) return '-';
  return Number(value).toLocaleString('en-US', { maximumFractionDigits: 2 });
}

function renderFetchLatencyRows(rows) {
  const body = document.getElementById('fetch-latency-body');
  if (!body) return;
  body.innerHTML = rows
    .map(
      (row) => `
        <tr class="border-b border-slate-100">
          <td class="px-4 py-3 text-slate-600">${escapeHtml(row.date)}</td>
          <td class="px-4 py-3 text-slate-800">${row.count}</td>
          <td class="px-4 py-3 text-slate-800">${formatMs(row.avg_ms)}</td>
          <td class="px-4 py-3 text-slate-600">${formatMs(row.p95_ms)}</td>
          <td class="px-4 py-3 text-slate-600">${formatMs(row.p99_ms)}</td>
        </tr>`,
    )
    .join('');
  if (rows.length === 0) {
    body.innerHTML = '<tr><td colspan="5" class="px-4 py-8 text-center text-slate-500">No data yet.</td></tr>';
  }
}

function renderFetchLatencyChart(data) {
  const canvas = document.getElementById('fetch-latency-chart');
  if (!canvas || typeof Chart === 'undefined') return;
  const labels = data.rows.map((row) => row.date);
  const datasets = [
    { label: 'Avg', data: data.rows.map((row) => row.avg_ms), borderColor: '#0f766e', backgroundColor: 'rgba(15, 118, 110, 0.15)', tension: 0.2 },
    { label: 'p95', data: data.rows.map((row) => row.p95_ms), borderColor: '#b45309', backgroundColor: 'rgba(180, 83, 9, 0.15)', tension: 0.2 },
    { label: 'p99', data: data.rows.map((row) => row.p99_ms), borderColor: '#b91c1c', backgroundColor: 'rgba(185, 28, 28, 0.15)', tension: 0.2 },
  ];
  const existing = Chart.getChart(canvas);
  if (existing) existing.destroy();
  new Chart(canvas, {
    type: 'line',
    data: { labels, datasets },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: 'index', intersect: false },
      scales: {
        y: { beginAtZero: true, title: { display: true, text: 'ms' } },
      },
      plugins: {
        legend: { position: 'bottom' },
        tooltip: { callbacks: { label: (ctx) => ` ${ctx.dataset.label}: ${formatMs(ctx.parsed.y)} ms` } },
      },
    },
  });
}

function renderFetchLatency(data) {
  renderFetchLatencyRows(data.rows);
  renderFetchLatencyChart(data);
}

async function main() {
  const statusEl = document.getElementById('metrics-error');
  const daysEl = document.getElementById('metrics-days');
  const tabDaily = document.getElementById('tab-daily');
  const tabLatency = document.getElementById('tab-fetch-latency');
  const tabGuest = document.getElementById('tab-guest');
  const viewDaily = document.getElementById('view-daily');
  const viewLatency = document.getElementById('view-fetch-latency');
  const viewGuest = document.getElementById('view-guest');

  const setActiveTab = (name) => {
    const tabs = [
      ['tab-daily', 'view-daily', 'daily'],
      ['tab-guest', 'view-guest', 'guest'],
      ['tab-fetch-latency', 'view-fetch-latency', 'latency'],
    ];
    for (const [tabId, viewId, key] of tabs) {
      const tab = document.getElementById(tabId);
      const view = document.getElementById(viewId);
      if (tab) tab.setAttribute('aria-selected', String(key === name));
      if (view) view.classList.toggle('hidden', key !== name);
    }
  };

  const currentView = () => {
    if (tabLatency && tabLatency.getAttribute('aria-selected') === 'true') return 'latency';
    if (tabGuest && tabGuest.getAttribute('aria-selected') === 'true') return 'guest';
    return 'daily';
  };

  const showError = (msg) => {
    if (!statusEl) return;
    statusEl.textContent = msg;
    statusEl.classList.remove('hidden');
  };
  const clearError = () => {
    if (!statusEl) return;
    statusEl.textContent = '';
    statusEl.classList.add('hidden');
  };

  const load = async (view) => {
    try {
      if (view === 'latency') {
        const data = await loadFetchLatency(daysEl ? daysEl.value : 14);
        renderFetchLatency(data);
      } else if (view === 'guest') {
        const data = await loadGuestMetrics(daysEl ? daysEl.value : 14);
        renderGuestMetrics(data);
      } else {
        const data = await loadMetrics(daysEl ? daysEl.value : 14);
        renderMetrics(data);
      }
      clearError();
    } catch (error) {
      if (error instanceof window.SessionExpiredError) {
        window.location.href = '/login?next=' + encodeURIComponent(
          window.location.pathname + window.location.search
        );
        return;
      }
      showError(error.message || String(error));
      // Keep body from staying in perpetual "Loading..." state
      const bodyId = view === 'latency' ? 'fetch-latency-body' : view === 'guest' ? 'guest-body' : 'metrics-body';
      const body = document.getElementById(bodyId);
      if (body && body.textContent.includes('Loading')) {
        const col = view === 'latency' ? 5 : view === 'guest' ? 7 : 6;
        body.innerHTML = `<tr><td colspan="${col}" class="px-4 py-6 text-center text-red-500">Failed to load. ${escapeHtml(error.message || '')}</td></tr>`;
      }
    }
  };

  if (tabDaily) tabDaily.addEventListener('click', () => { setActiveTab('daily'); load('daily'); });
  if (tabLatency) tabLatency.addEventListener('click', () => { setActiveTab('latency'); load('latency'); });
  if (tabGuest) tabGuest.addEventListener('click', () => { setActiveTab('guest'); load('guest'); });
  if (daysEl) daysEl.addEventListener('change', () => load(currentView()));
  await load(currentView());
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', main);
} else {
  main();
}

window.__test = { renderMetrics, renderFetchLatency, renderFetchLatencyRows, loadMetrics, loadFetchLatency, renderGuestMetrics, loadGuestMetrics };
