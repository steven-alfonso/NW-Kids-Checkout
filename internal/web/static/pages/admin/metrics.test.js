import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { JSDOM, VirtualConsole } from 'jsdom';

const scriptPath = path.resolve(process.cwd(), 'internal/web/static/pages/admin/metrics.js');
const apiScriptPath = path.resolve(process.cwd(), 'internal/web/static/js/api.js');
const script = fs.readFileSync(scriptPath, 'utf8');
const apiScript = fs.readFileSync(apiScriptPath, 'utf8');
const exposeInternals = `
window.__test = { renderMetrics, renderFetchLatency, renderFetchLatencyRows, loadMetrics, loadFetchLatency, renderGuestMetrics, loadGuestMetrics };
`;

const fixtureHtml = `<!doctype html>
    <html>
        <body>
            <div id="page-status" class="hidden"></div>
            <div id="metrics-error" class="hidden"></div>
            <select id="metrics-days"><option value="7">7</option><option value="14" selected>14</option><option value="30">30</option></select>
            <table>
                <tbody id="metrics-body"></tbody>
            </table>
            <div id="tab-daily" role="tab" aria-selected="true"></div>
            <div id="tab-fetch-latency" role="tab" aria-selected="false"></div>
            <div id="tab-guest" role="tab" aria-selected="false"></div>
            <div id="view-daily"></div>
            <div id="view-fetch-latency" class="hidden"></div>
            <div id="view-guest" class="hidden"></div>
            <div id="fetch-latency-body"></div>
            <canvas id="fetch-latency-chart"></canvas>
            <div id="guest-body"></div>
        </body>
    </html>`;

const defaultFetch = async () => ({
    ok: true,
    status: 200,
    json: async () => ({ days: 14, daily: [] }),
    text: async () => ''
});

function loadWindow(fetchImpl = defaultFetch) {
    const virtualConsole = new VirtualConsole();
    virtualConsole.on('jsdomError', (e) => {
        if (e.message.includes('Not implemented: navigation')) return;
        console.error(e);
    });
    const dom = new JSDOM(fixtureHtml, {
        runScripts: 'dangerously',
        url: 'http://localhost/',
        virtualConsole
    });
    dom.window.fetch = fetchImpl;
    dom.window.setInterval = () => 0;
    dom.window.eval(apiScript);
    dom.window.eval(script);
    dom.window.eval(exposeInternals);
    return dom.window;
}

describe('admin/metrics', () => {
    it('renderMetrics renders rows and empty state', () => {
        const window = loadWindow();
        window.__test.renderMetrics({
            days: 14,
            daily: [
                { date: '2026-08-18', event_name: 'Kids', called: 5, confirmed: 4, unconfirmed: 1, avg_confirm_minutes: 3.5 },
            ],
        });
        let html = window.document.getElementById('metrics-body').innerHTML;
        expect(html).toContain('Kids');
        expect(html).toContain('3.5');
        expect(html).toContain('2026-08-18');

        window.__test.renderMetrics({ days: 14, daily: [] });
        html = window.document.getElementById('metrics-body').innerHTML;
        expect(html).toContain('No data yet.');
        expect(html).toContain('colspan="6"');
    });

    it('loadMetrics builds URL with days param', async () => {
        const calls = [];
        const fetchImpl = async (url) => {
            calls.push(url);
            return { ok: true, status: 200, json: async () => ({ days: 7, daily: [] }), text: async () => '' };
        };
        const window = loadWindow(fetchImpl);
        const data = await window.__test.loadMetrics(7);
        expect(calls[0]).toContain('days=7');
        expect(data.days).toBe(7);
    });

    it('loadFetchLatency builds URL with fetch-latency path and days param', async () => {
        const calls = [];
        const fetchImpl = async (url) => {
            calls.push(url);
            return { ok: true, status: 200, json: async () => ({ days: 14, rows: [] }), text: async () => '' };
        };
        const window = loadWindow(fetchImpl);
        const data = await window.__test.loadFetchLatency(14);
        expect(calls[0]).toContain('/fetch-latency?days=14');
        expect(data.days).toBe(14);
    });

    it('loadGuestMetrics builds URL with guest path and days param', async () => {
        const calls = [];
        const fetchImpl = async (url) => {
            calls.push(url);
            return { ok: true, status: 200, json: async () => ({ days: 14, rows: [] }), text: async () => '' };
        };
        const window = loadWindow(fetchImpl);
        const data = await window.__test.loadGuestMetrics(14);
        expect(calls[0]).toContain('/guest?days=14');
        expect(data.days).toBe(14);
    });

    it('renderGuestMetrics renders rows and empty state', () => {
        const window = loadWindow();
        window.__test.renderGuestMetrics({
            days: 14,
            rows: [
                { date: '2026-08-18', submissions: 5, children: 9, entered: 2, approved: 1, rejected: 1, pending: 1 },
            ],
        });
        let html = window.document.getElementById('guest-body').innerHTML;
        expect(html).toContain('2026-08-18');
        expect(html).toContain('5');
        expect(html).toContain('9');
        expect(html).toContain('2');
        expect(html).toContain('1');

        window.__test.renderGuestMetrics({ days: 14, rows: [] });
        html = window.document.getElementById('guest-body').innerHTML;
        expect(html).toContain('No data yet.');
    });

    it('renderFetchLatency renders table rows and empty state', () => {
        const window = loadWindow();
        window.__test.renderFetchLatency({
            days: 14,
            rows: [
                { date: '2026-08-18', count: 3, avg_ms: 1500, p95_ms: 2500, p99_ms: 3000 },
            ],
        });
        let html = window.document.getElementById('fetch-latency-body').innerHTML;
        expect(html).toContain('2026-08-18');
        expect(html).toContain('1,500');
        expect(html).toContain('2,500');
        expect(html).toContain('3,000');

        window.__test.renderFetchLatency({ days: 14, rows: [] });
        html = window.document.getElementById('fetch-latency-body').innerHTML;
        expect(html).toContain('No data yet.');
    });

    it('main switches tabs and fetches the right endpoint', async () => {
        const calls = [];
        const fetchImpl = async (url) => {
            calls.push(url);
            return { ok: true, status: 200, json: async () => ({ days: 14, daily: [], rows: [] }), text: async () => '' };
        };
        const window = loadWindow(fetchImpl);
        await new Promise((resolve) => setTimeout(resolve, 0));

        // Initial load hits the daily endpoint.
        expect(calls[0]).toContain('/v1/admin/metrics?days=14');

        const latencyTab = window.document.getElementById('tab-fetch-latency');
        latencyTab.click();
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(calls[1]).toContain('/v1/admin/metrics/fetch-latency?days=14');
        expect(window.document.getElementById('view-fetch-latency').classList.contains('hidden')).toBe(false);
        expect(window.document.getElementById('view-daily').classList.contains('hidden')).toBe(true);

        const dailyTab = window.document.getElementById('tab-daily');
        dailyTab.click();
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(calls[2]).toContain('/v1/admin/metrics?days=14');
        expect(window.document.getElementById('view-daily').classList.contains('hidden')).toBe(false);
        expect(window.document.getElementById('view-fetch-latency').classList.contains('hidden')).toBe(true);
    });

    it('main switches to the guest tab and fetches the guest endpoint', async () => {
        const calls = [];
        const fetchImpl = async (url) => {
            calls.push(url);
            return { ok: true, status: 200, json: async () => ({ days: 14, daily: [], rows: [] }), text: async () => '' };
        };
        const window = loadWindow(fetchImpl);
        await new Promise((resolve) => setTimeout(resolve, 0));

        expect(calls[0]).toContain('/v1/admin/metrics?days=14');

        const guestTab = window.document.getElementById('tab-guest');
        guestTab.click();
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(calls[1]).toContain('/v1/admin/metrics/guest?days=14');
        expect(window.document.getElementById('view-guest').classList.contains('hidden')).toBe(false);
        expect(window.document.getElementById('view-daily').classList.contains('hidden')).toBe(true);
        expect(window.document.getElementById('view-fetch-latency').classList.contains('hidden')).toBe(true);

        const latencyTab = window.document.getElementById('tab-fetch-latency');
        latencyTab.click();
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(calls[2]).toContain('/v1/admin/metrics/fetch-latency?days=14');
        expect(window.document.getElementById('view-guest').classList.contains('hidden')).toBe(true);
    });

    it('does not show cryptic JSON error when metrics fetch is HTML login redirect', async () => {
        const fetchImpl = async () => ({ ok: true, redirected: true, url: 'http://localhost/login', status: 200, headers: { get: () => 'text/html' }, json: async () => { throw new Error('Unexpected token <'); } });
        const window = loadWindow(fetchImpl);
        await new Promise((resolve) => setTimeout(resolve, 0));
        const errEl = window.document.getElementById('metrics-error');
        expect(errEl.textContent).not.toContain('Unexpected token');
        expect(errEl.textContent).not.toContain('<');
    });

    it('loadMetrics throws SessionExpiredError on HTML response', async () => {
        const fetchImpl = async () => ({ ok: false, redirected: false, url: 'http://localhost/v1/admin/metrics?days=14', status: 200, headers: { get: () => 'text/html; charset=utf-8' }, json: async () => { throw new Error('no json'); } });
        const window = loadWindow(fetchImpl);
        await expect(window.__test.loadMetrics(14)).rejects.toThrow('Session expired');
        try {
            await window.__test.loadMetrics(14);
        } catch (e) {
            expect(e.name).toBe('SessionExpiredError');
        }
    });

    it('loadGuestMetrics throws SessionExpiredError on redirected login', async () => {
        const fetchImpl = async () => ({ ok: true, redirected: true, url: 'http://localhost/login', status: 200, headers: { get: () => 'text/html' }, json: async () => ({}) });
        const window = loadWindow(fetchImpl);
        await expect(window.__test.loadGuestMetrics(14)).rejects.toThrow('Session expired');
    });

    it('loadFetchLatency throws SessionExpiredError on HTML content-type', async () => {
        const fetchImpl = async () => ({ ok: true, redirected: false, url: 'http://localhost/v1/admin/metrics/fetch-latency?days=14', status: 200, headers: { get: () => 'text/html' }, json: async () => ({}) });
        const window = loadWindow(fetchImpl);
        await expect(window.__test.loadFetchLatency(14)).rejects.toThrow('Session expired');
    });
});