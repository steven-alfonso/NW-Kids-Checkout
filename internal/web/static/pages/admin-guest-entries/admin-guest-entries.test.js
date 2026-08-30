import {describe, it, expect} from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import {JSDOM} from 'jsdom';

const scriptPath = path.resolve(process.cwd(), 'internal/web/static/pages/admin-guest-entries/admin-guest-entries.js');
const apiScriptPath = path.resolve(process.cwd(), 'internal/web/static/js/api.js');
const script = fs.readFileSync(scriptPath, 'utf8');
const apiScript = fs.readFileSync(apiScriptPath, 'utf8');
const flush = () => new Promise(resolve => setTimeout(resolve, 0));

function loadWindow({respond} = {}) {
    const dom = new JSDOM(`<!doctype html>
        <html>
        <body>
            <div class="inline-flex rounded-lg border border-slate-200 bg-white p-1 shadow-sm" role="tablist">
                <button id="tab-needs-entry" role="tab" aria-selected="true">Needs Planning Center Entry</button>
                <button id="tab-entered" role="tab" aria-selected="false">Entered</button>
                <button id="tab-rejected" role="tab" aria-selected="false">Rejected</button>
            </div>
            <div id="entries-container">
                <div id="view-needs-entry">
                    <div id="pagination-needs-entry-top" class="mb-4"></div>
                    <div id="entries-needs-entry" class="space-y-4"></div>
                    <div id="pagination-needs-entry-bottom" class="mt-4"></div>
                </div>
                <div id="view-entered" class="hidden">
                    <div id="pagination-entered-top" class="mb-4"></div>
                    <div id="entries-entered" class="space-y-4"></div>
                    <div id="pagination-entered-bottom" class="mt-4"></div>
                </div>
                <div id="view-rejected" class="hidden">
                    <div id="pagination-rejected-top" class="mb-4"></div>
                    <div id="entries-rejected" class="space-y-4"></div>
                    <div id="pagination-rejected-bottom" class="mt-4"></div>
                </div>
            </div>
            <div id="page-status" class="hidden"></div>
        </body></html>`, {runScripts: 'dangerously', url: 'http://localhost/'});
    const calls = [];
    dom.window.fetch = async (url, opts) => {
        calls.push({url: String(url), opts: opts || {}});
        if (respond) return respond(url, opts);
        return {ok: true, status: 200, json: async () => pageOf([])};
    };
    dom.window.navigator.clipboard = {writeText: async (text) => (dom.window._copied = text)};
    dom.window.eval(apiScript);
    dom.window.eval(script);
    dom.window._fetchCalls = calls;
    return dom.window;
}

function entry(publicId, status, overrides = {}) {
    return {
        public_id: publicId,
        status,
        parent: {first_name: 'A', last_name: 'B', phone: '555', email: 'a@b.com'},
        children: [],
        ...overrides
    };
}

function pageOf(items, overrides = {}) {
    return {
        items,
        total: items.length,
        page: 1,
        page_size: 10,
        total_pages: items.length ? 1 : 0,
        ...overrides
    };
}

describe('admin-guest-entries', () => {
    describe('tabs', () => {
        it('loads only the needs-entry tab on initial page load', async () => {
            const window = loadWindow();
            await flush();
            const urls = window._fetchCalls.map(c => c.url);
            expect(urls).toContain('/v1/admin/guest-submissions?status=pending&page=1');
            expect(urls).toContain('/v1/admin/guest-submissions?status=approved&page=1');
            expect(urls).not.toContain('/v1/admin/guest-submissions?status=entered');
            expect(urls).not.toContain('/v1/admin/guest-submissions?status=rejected');
        });

        it('fetches the entered tab only after it is clicked', async () => {
            const window = loadWindow();
            await flush();
            expect(window._fetchCalls.some(c => c.url.includes('status=entered'))).toBe(false);
            window.document.getElementById('tab-entered').click();
            await flush();
            expect(window._fetchCalls.some(c => c.url.includes('status=entered'))).toBe(true);
            expect(window._fetchCalls.some(c => c.url.includes('status=rejected'))).toBe(false);
        });

        it('marks the clicked tab active and shows its view', async () => {
            const window = loadWindow();
            const byId = (id) => window.document.getElementById(id);
            expect(byId('tab-needs-entry').getAttribute('aria-selected')).toBe('true');
            expect(byId('view-entered').classList.contains('hidden')).toBe(true);
            expect(byId('view-needs-entry').classList.contains('hidden')).toBe(false);

            byId('tab-entered').click();
            await flush();

            expect(byId('tab-entered').getAttribute('aria-selected')).toBe('true');
            expect(byId('tab-needs-entry').getAttribute('aria-selected')).toBe('false');
            expect(byId('view-entered').classList.contains('hidden')).toBe(false);
            expect(byId('view-needs-entry').classList.contains('hidden')).toBe(true);
        });

        it('renders tab entries without a count heading', async () => {
            const window = loadWindow({
                respond: (url) => url.includes('status=entered')
                    ? {ok: true, status: 200, json: async () => pageOf([entry('e-1', 'entered'), entry('e-2', 'entered')])}
                    : {ok: true, status: 200, json: async () => pageOf([])}
            });
            await window.loadEntries('entered');
            const heading = window.document.querySelector('#view-entered h2');
            expect(heading).toBeNull();
        });

        it('shows an empty state when a tab has no entries', async () => {
            const window = loadWindow();
            await window.loadEntries('rejected');
            const view = window.document.getElementById('view-rejected');
            expect(view.textContent).toContain('No guest entries yet.');
        });

        it('renders entries into the active tab view', async () => {
            const window = loadWindow({
                respond: (url) => {
                    if (url.includes('status=pending')) return {ok: true, status: 200, json: async () => pageOf([entry('p-1', 'pending')])};
                    return {ok: true, status: 200, json: async () => pageOf([entry('a-1', 'approved')])};
                }
            });
            await window.loadEntries('needs-entry');
            const view = window.document.getElementById('view-needs-entry');
            expect(view.querySelectorAll('.rounded-xl').length).toBe(2);
        });

        it('reloads the active tab after marking an entry entered', async () => {
            const window = loadWindow({
                respond: (url, opts) => {
                    if (opts && opts.method === 'PATCH') return {ok: true, status: 200, json: async () => ({})};
                    if (url.includes('status=pending')) return {ok: true, status: 200, json: async () => pageOf([entry('s-1', 'pending')])};
                    return {ok: true, status: 200, json: async () => pageOf([])};
                }
            });
            await flush();
            const countPending = () => window._fetchCalls.filter(c => !c.opts.method && c.url.includes('status=pending')).length;
            expect(countPending()).toBe(1);

            window.document.querySelector('[data-mark-entered]').click();
            await flush();

            expect(countPending()).toBe(2);
            expect(window._fetchCalls.some(c => c.opts.method === 'PATCH' && c.url.includes('/s-1/status'))).toBe(true);
        });
    });

    describe('entry rendering', () => {
        it('renders each field as a labeled click-to-copy chip', () => {
            const window = loadWindow();
            const container = window.document.createElement('div');
            const sub = entry('sub-1', 'approved', {
                parent: {first_name: 'John', last_name: 'Smith', phone: '555-1234', email: 'j@e.com'},
                children: [{first_name: 'Timmy', last_name: 'Smith', dob: '2020-01-01', grade: '1st Grade'}]
            });
            const chips = window.renderEntry(container, sub);
            expect(container.querySelectorAll('[data-copy]').length).toBeGreaterThanOrEqual(8);
            expect(chips[0].dataset.copy).toBe('John');

            const labels = Array.from(container.querySelectorAll('span.text-xs')).map(el => el.textContent);
            expect(labels).toContain('first name');
            expect(labels).toContain('email');
            expect(labels).toContain('grade');
        });

        it('renders missing phone/email as an em dash on parent chips', () => {
            const window = loadWindow();
            const container = window.document.createElement('div');
            const sub = entry('sub-4', 'approved', {parent: {first_name: 'A', last_name: 'B', phone: '', email: ''}});
            window.renderEntry(container, sub);
            const phoneChip = Array.from(container.querySelectorAll('[data-copy]')).find(btn => btn.dataset.label === 'phone');
            const emailChip = Array.from(container.querySelectorAll('[data-copy]')).find(btn => btn.dataset.label === 'email');
            expect(phoneChip.textContent).toBe('—');
            expect(emailChip.textContent).toBe('—');
        });

        it('shows a Copied tooltip on the field after copying', () => {
            const window = loadWindow();
            const container = window.document.createElement('div');
            window.renderEntry(container, entry('sub-3', 'approved'));
            const chipBtn = container.querySelector('[data-copy]');
            const field = chipBtn.closest('.relative');
            window.showCopiedTooltip(field);

            const tip = field.querySelector('.copy-tooltip');
            expect(tip).not.toBeNull();
            expect(tip.textContent).toBe('Copied');
        });

        it('shows only the most recently clicked Copied tooltip', () => {
            const window = loadWindow();
            const container = window.document.createElement('div');
            window.renderEntry(container, entry('sub-3', 'approved'));
            const fields = Array.from(container.querySelectorAll('[data-copy]')).map(btn => btn.closest('.relative'));
            window.showCopiedTooltip(fields[0]);
            window.showCopiedTooltip(fields[1]);

            expect(fields[0].querySelector('.copy-tooltip')).toBeNull();
            expect(fields[1].querySelector('.copy-tooltip')).not.toBeNull();
        });

        it('copies a single value', async () => {
            const window = loadWindow();
            await window.copyValue('Timmy');
            expect(window._copied).toBe('Timmy');
        });

        it('uses distinct badge colors per bucket', () => {
            const window = loadWindow();
            const container = window.document.createElement('div');
            window.renderEntry(container, entry('sub-2', 'approved'));
            const badge = container.querySelector('.rounded-full');
            expect(badge.className).toContain('bg-amber-100');
            expect(badge.className).toContain('text-amber-800');
            expect(badge.textContent).toBe('Needs Planning Center Entry');
        });

        it('adds a status-colored left bar to needs-entry and rejected cards', () => {
            const window = loadWindow();
            const container = window.document.createElement('div');
            window.renderEntry(container, entry('appr-1', 'approved'));
            window.renderEntry(container, entry('pend-1', 'pending'));
            window.renderEntry(container, entry('rej-1', 'rejected', {parent: {first_name: 'E', last_name: 'F', phone: '3', email: 'e@f.com'}}));
            const cards = container.querySelectorAll('.rounded-xl');
            expect(cards[0].className).toContain('border-l-amber-400');
            expect(cards[1].className).toContain('border-l-amber-400');
            expect(cards[2].className).toContain('border-l-red-400');
        });

        it('renders the mark-entered button on approved cards', () => {
            const window = loadWindow();
            const container = window.document.createElement('div');
            window.renderEntry(container, entry('appr-1', 'approved'));
            const btn = container.querySelector('[data-mark-entered="appr-1"]');
            expect(btn).not.toBeNull();
        });

        it('renders the mark-entered button on pending cards', () => {
            const window = loadWindow();
            const container = window.document.createElement('div');
            window.renderEntry(container, entry('pend-1', 'pending'));
            const btn = container.querySelector('[data-mark-entered="pend-1"]');
            expect(btn).not.toBeNull();
        });
    });

    describe('pagination', () => {
        it('fetches the clicked page and renders its entries', async () => {
            const window = loadWindow({
                respond: (url) => {
                    const page = Number(new URL(url, 'http://localhost').searchParams.get('page')) || 1;
                    if (url.includes('status=pending')) {
                        const item = page === 1
                            ? entry('p-1', 'pending', {parent: {first_name: 'Page', last_name: 'One', phone: '1', email: 'a@b.com'}})
                            : entry('p-2', 'pending', {parent: {first_name: 'Page', last_name: 'Two', phone: '2', email: 'a@b.com'}});
                        return {ok: true, status: 200, json: async () => pageOf([item], {page, total_pages: 2, total: 15})};
                    }
                    return {ok: true, status: 200, json: async () => pageOf([], {page, total_pages: 2, total: 12})};
                }
            });
            await flush();
            expect(window._fetchCalls.some(c => c.url.includes('page=1'))).toBe(true);

            const next = Array.from(window.document.querySelectorAll('[data-goto-page]')).find(b => b.textContent === 'Next ›');
            next.click();
            await flush();

            expect(window._fetchCalls.some(c => c.url.includes('page=2'))).toBe(true);
            const cards = window.document.querySelectorAll('#entries-needs-entry .rounded-xl');
            expect(cards.length).toBe(1);
            expect(cards[0].textContent).toContain('Page Two');
        });

        it('renders page summary and navigation in both top and bottom bars', async () => {
            const window = loadWindow({
                respond: (url) => url.includes('status=pending')
                    ? {ok: true, status: 200, json: async () => pageOf([entry('p-1', 'pending'), entry('p-2', 'pending')], {total: 15, total_pages: 2})}
                    : {ok: true, status: 200, json: async () => pageOf([], {total: 12, total_pages: 2})}
            });
            await window.loadEntries('needs-entry');

            const summary = 'Page 1 of 2 · 27 entries';
            expect(window.document.getElementById('pagination-needs-entry-top').textContent).toContain(summary);
            expect(window.document.getElementById('pagination-needs-entry-bottom').textContent).toContain(summary);
            expect(window.document.querySelectorAll('[data-goto-page]').length).toBeGreaterThan(0);
        });

        it('hides pagination bars when a tab has no entries', async () => {
            const window = loadWindow();
            await window.loadEntries('rejected');
            expect(window.document.getElementById('pagination-rejected-top').classList.contains('hidden')).toBe(true);
            expect(window.document.getElementById('pagination-rejected-bottom').classList.contains('hidden')).toBe(true);
        });

        it('steps back to the last non-empty page', async () => {
            const window = loadWindow({
                respond: (url) => {
                    const page = Number(new URL(url, 'http://localhost').searchParams.get('page')) || 1;
                    if (url.includes('status=pending')) {
                        return {ok: true, status: 200, json: async () => pageOf(page === 1 ? [entry('p-1', 'pending')] : [], {page, total_pages: 2, total: 11})};
                    }
                    return {ok: true, status: 200, json: async () => pageOf([], {page, total_pages: 2, total: 11})};
                }
            });
            await flush();
            window.document.querySelector('[data-goto-page="2"]').click();
            await flush();

            const page1Fetches = window._fetchCalls.filter(c => c.url.includes('page=1')).length;
            expect(page1Fetches).toBeGreaterThanOrEqual(2);
            expect(window.document.querySelectorAll('#entries-needs-entry .rounded-xl').length).toBe(1);
        });

        it('sorts merged pending + approved entries newest first', async () => {
            const window = loadWindow({
                respond: (url) => {
                    if (url.includes('status=pending')) {
                        return {ok: true, status: 200, json: async () => pageOf([entry('old-pending', 'pending', {
                            created_at: '2026-01-01T10:00:00Z',
                            parent: {first_name: 'Old', last_name: 'Pending', phone: '1', email: 'a@b.com'}
                        })])};
                    }
                    return {ok: true, status: 200, json: async () => pageOf([
                        entry('new-approved', 'approved', {
                            created_at: '2026-02-01T10:00:00Z',
                            parent: {first_name: 'New', last_name: 'Approved', phone: '1', email: 'a@b.com'}
                        }),
                        entry('no-date-approved', 'approved', {
                            created_at: null,
                            parent: {first_name: 'No', last_name: 'Date', phone: '1', email: 'a@b.com'}
                        })
                    ])};
                }
            });
            await window.loadEntries('needs-entry');
            const cards = window.document.querySelectorAll('#entries-needs-entry .rounded-xl');
            expect(cards.length).toBe(3);
            expect(cards[0].textContent).toContain('New Approved');
            expect(cards[1].textContent).toContain('Old Pending');
            expect(cards[2].textContent).toContain('No Date');
        });

        it('keeps the numbered window at a consistent 5 pages', async () => {
            const window = loadWindow({
                respond: (url) => {
                    const page = Number(new URL(url, 'http://localhost').searchParams.get('page')) || 1;
                    if (url.includes('status=pending')) {
                        return {ok: true, status: 200, json: async () => pageOf([entry(`p-${page}`, 'pending', {
                            parent: {first_name: `P${page}`, last_name: 'Familia', phone: '1', email: 'a@b.com'}
                        })], {page, total_pages: 30, total: 300})};
                    }
                    return {ok: true, status: 200, json: async () => pageOf([], {page, total_pages: 30, total: 300})};
                }
            });
            const pageNumbers = () => {
                const nums = new Set();
                window.document.querySelectorAll('[data-goto-page]').forEach(btn => {
                    const n = parseInt(btn.dataset.gotoPage, 10);
                    if (Number.isFinite(n)) nums.add(n);
                });
                return nums;
            };

            await flush();
            let nums = pageNumbers();
            for (let i = 1; i <= 5; i++) expect(nums.has(i)).toBe(true);
            expect(nums.has(6)).toBe(false);
            expect(nums.has(30)).toBe(true);

            Array.from(window.document.querySelectorAll('[data-goto-page]')).find(b => b.textContent === 'Last').click();
            await flush();
            nums = pageNumbers();
            for (let i = 26; i <= 30; i++) expect(nums.has(i)).toBe(true);
            expect(nums.has(25)).toBe(false);
            expect(nums.has(1)).toBe(true);
        });

        it('does not show success status when session expires during mark entered', async () => {
            const window = loadWindow({
                respond: (url, opts) => {
                    if (opts && opts.method === 'PATCH') {
                        return {ok: true, redirected: true, url: 'http://localhost/login', status: 200, headers: {get: () => 'text/html'}};
                    }
                    if (url.includes('status=pending')) return {ok: true, status: 200, json: async () => pageOf([entry('s-1', 'pending')])};
                    return {ok: true, status: 200, json: async () => pageOf([])};
                }
            });
            await flush();
            const markEnteredBtn = window.document.querySelector('[data-mark-entered]');
            expect(markEnteredBtn).not.toBeNull();
            markEnteredBtn.click();
            await flush();
            const statusEl = window.document.getElementById('page-status');
            expect(statusEl.classList.contains('hidden')).toBe(true);
            expect(statusEl.textContent).toBe('');
            const entriesContainer = window.document.getElementById('entries-needs-entry');
            expect(entriesContainer.innerHTML).not.toContain('No guest entries yet.');
            const btn = window.document.querySelector('[data-mark-entered]');
            expect(btn.disabled).toBe(false);
        });

        it('shows parsed sorry message when mark entered fails', async () => {
            const window = loadWindow({
                respond: (url, opts) => {
                    if (opts && opts.method === 'PATCH') {
                        return {
                            ok: false,
                            status: 400,
                            redirected: false,
                            url: 'http://localhost/v1/checkins/guest-submissions/s-1/status',
                            headers: {get: () => 'application/json'},
                            json: async () => ({sorry: 'submission status changed, please retry'})
                        };
                    }
                    if (url.includes('status=pending')) return {ok: true, status: 200, json: async () => pageOf([entry('s-1', 'pending')])};
                    return {ok: true, status: 200, json: async () => pageOf([])};
                }
            });
            await flush();
            const btn = window.document.querySelector('[data-mark-entered]');
            expect(btn).not.toBeNull();
            btn.click();
            await flush();
            await flush();
            const statusEl = window.document.getElementById('page-status');
            expect(statusEl.textContent).toContain('submission status changed, please retry');
            expect(statusEl.textContent).not.toContain('{"sorry"');
            expect(statusEl.textContent).not.toContain('Failed to mark entered');
            expect(window.document.querySelector('[data-mark-entered]').disabled).toBe(false);
        });

        it('surfaces invalid transition sorry without raw JSON', async () => {
            const window = loadWindow({
                respond: (url, opts) => {
                    if (opts && opts.method === 'PATCH') {
                        return {
                            ok: false,
                            status: 400,
                            redirected: false,
                            url: 'http://localhost/v1/checkins/guest-submissions/s-1/status',
                            headers: {get: () => 'application/json'},
                            json: async () => ({sorry: 'invalid status transition'})
                        };
                    }
                    if (url.includes('status=pending')) return {ok: true, status: 200, json: async () => pageOf([entry('s-1', 'pending')])};
                    return {ok: true, status: 200, json: async () => pageOf([])};
                }
            });
            await flush();
            window.document.querySelector('[data-mark-entered]').click();
            await flush();
            await flush();
            const statusEl = window.document.getElementById('page-status');
            expect(statusEl.textContent).toContain('invalid status transition');
            expect(statusEl.textContent).not.toContain('{"sorry"');
        });
    });
});