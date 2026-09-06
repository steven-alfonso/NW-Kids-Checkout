const API_URL = '';

function parseErrorBody(data, fallback) {
  if (data && typeof data === 'object') {
    return data.message || data.error || data.sorry || fallback;
  }
  if (typeof data === 'string' && data.trim()) return data.trim();
  return fallback;
}

async function fetchJsonWithError(url, fallbackPrefix) {
  const response = await fetch(url, { credentials: 'same-origin' });
  if (!response.ok) {
    const text = await response.text().catch(() => '');
    let data;
    try { data = JSON.parse(text); } catch (_) { data = text; }
    throw new Error(parseErrorBody(data, `${fallbackPrefix} (${response.status})`));
  }
  try {
    return await response.json();
  } catch (e) {
    const text = await response.text().catch(() => '');
    if (text && text.trim().startsWith('<')) {
      throw new Error(`${fallbackPrefix}: received HTML instead of JSON (maybe not authenticated)`);
    }
    throw new Error(`${fallbackPrefix}: invalid JSON response`);
  }
}

async function loadMetrics(days) {
  return fetchJsonWithError(`${API_URL}/v1/admin/metrics?days=${days}`, 'failed to load metrics');
}

async function loadFetchLatency(days) {
  return fetchJsonWithError(`${API_URL}/v1/admin/metrics/fetch-latency?days=${days}`, 'failed to load fetch latency');
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
          <td class="px-4 py-3 text-slate-600">${m.manual_count}</td>
        </tr>`,
    )
    .join('');
  if (data.daily.length === 0) {
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
  const viewDaily = document.getElementById('view-daily');
  const viewLatency = document.getElementById('view-fetch-latency');

  const setTab = (latency) => {
    if (tabDaily) tabDaily.setAttribute('aria-selected', String(!latency));
    if (tabLatency) tabLatency.setAttribute('aria-selected', String(latency));
    if (viewDaily) viewDaily.classList.toggle('hidden', latency);
    if (viewLatency) viewLatency.classList.toggle('hidden', !latency);
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

  const load = async (latency) => {
    try {
      if (latency) {
        const data = await loadFetchLatency(daysEl ? daysEl.value : 14);
        renderFetchLatency(data);
      } else {
        const data = await loadMetrics(daysEl ? daysEl.value : 14);
        renderMetrics(data);
      }
      clearError();
    } catch (error) {
      showError(error.message || String(error));
      // Keep body from staying in perpetual "Loading..." state
      const body = document.getElementById(latency ? 'fetch-latency-body' : 'metrics-body');
      if (body && body.textContent.includes('Loading')) {
        body.innerHTML = `<tr><td colspan="${latency ? 5 : 7}" class="px-4 py-6 text-center text-red-500">Failed to load. ${escapeHtml(error.message || '')}</td></tr>`;
      }
    }
  };

  if (tabDaily) tabDaily.addEventListener('click', () => { setTab(false); load(false); });
  if (tabLatency) tabLatency.addEventListener('click', () => { setTab(true); load(true); });
  if (daysEl) daysEl.addEventListener('change', () => load(tabLatency && tabLatency.getAttribute('aria-selected') === 'true'));
  await load(false);
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', main);
} else {
  main();
}

window.__test = { renderMetrics, renderFetchLatency, renderFetchLatencyRows, loadMetrics, loadFetchLatency, parseErrorBody, fetchJsonWithError };