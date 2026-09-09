const API_URL = '';
const STALE_THRESHOLD_MS = 15 * 60 * 1000;

async function loadEvents() {
  const response = await fetch(`${API_URL}/v1/events`, { credentials: 'same-origin' });
  if (!response.ok) {
    const text = await response.text().catch(() => '');
    let data;
    try { data = JSON.parse(text); } catch (_) { data = text; }
    const msg = (data && data.error) || (data && data.message) || (data && data.sorry) || text || `failed to load events (${response.status})`;
    throw new Error(msg);
  }
  try {
    return await response.json();
  } catch (_) {
    const text = await response.text().catch(() => '');
    if (text && text.trim().startsWith('<')) {
      throw new Error('failed to load events: received HTML instead of JSON (maybe not authenticated)');
    }
    throw new Error('failed to load events: invalid JSON response');
  }
}

function formatAge(ms) {
  if (ms < 60 * 1000) return `${Math.max(0, Math.floor(ms / 1000))}s`;
  if (ms < 60 * 60 * 1000) return `${Math.floor(ms / 60000)}m`;
  return `${Math.floor(ms / 3600000)}h`;
}

function escapeHtml(value) {
  return String(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

function renderEvents(events) {
  const body = document.getElementById('fetcher-status-body');
  const now = Date.now();
  const rows = events
    .map((event) => {
      const last = event.last_checked_out_time ? new Date(event.last_checked_out_time).getTime() : null;
      const stale = last !== null && now - last > STALE_THRESHOLD_MS;
      const autoFetch = event.auto_fetch;
      let status;
      if (last === null) {
        status = '<span class="rounded bg-slate-100 px-2 py-0.5 text-xs text-slate-600">never</span>';
      } else if (stale) {
        status = '<span class="rounded bg-red-100 px-2 py-0.5 text-xs font-medium text-red-700">stale</span>';
      } else {
        status = '<span class="rounded bg-green-100 px-2 py-0.5 text-xs font-medium text-green-700">ok</span>';
      }
      return `
        <tr class="border-b border-slate-100">
          <td class="px-4 py-3 text-slate-800">${escapeHtml(event.name)}</td>
          <td class="px-4 py-3">
            ${autoFetch ? '<span class="text-xs text-slate-600">yes</span>' : '<span class="text-xs text-slate-400">no</span>'}
          </td>
          <td class="px-4 py-3 text-slate-600">${last !== null ? new Date(last).toLocaleString() : '—'}</td>
          <td class="px-4 py-3">${last !== null ? formatAge(now - last) : '—'} ${status}</td>
        </tr>`;
    })
    .join('');
  body.innerHTML = rows;
}

async function main() {
  const statusEl = document.getElementById('fetcher-status-error');
  try {
    const events = await loadEvents();
    renderEvents(events);
    if (statusEl) {
      statusEl.textContent = '';
      statusEl.classList.add('hidden');
    }
  } catch (error) {
    if (statusEl) {
      statusEl.textContent = error.message;
      statusEl.classList.remove('hidden');
    }
    const body = document.getElementById('fetcher-status-body');
    if (body && body.textContent.includes('Loading')) {
      body.innerHTML = `<tr><td colspan="4" class="px-4 py-6 text-center text-red-500">Failed to load. ${escapeHtml(error.message || '')}</td></tr>`;
    }
  }
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', main);
} else {
  main();
}

// test hooks (mirror existing pattern)
window.__test = { renderEvents, loadEvents, formatAge };