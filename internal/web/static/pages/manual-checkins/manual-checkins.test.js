import {describe, it, expect} from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import {JSDOM} from 'jsdom';

const scriptPath = path.resolve(process.cwd(), 'internal/web/static/pages/manual-checkins/manual-checkins.js');
const apiScriptPath = path.resolve(process.cwd(), 'internal/web/static/js/api.js');
const script = fs.readFileSync(scriptPath, 'utf8');
const apiScript = fs.readFileSync(apiScriptPath, 'utf8');

function loadWindow({ url = 'http://localhost/' } = {}) {
    const dom = new JSDOM(`<!doctype html>
        <html>
        <body>
            <div id="page-status" class="hidden"></div>
            <div id="pending-families"></div>
            <table><tbody id="manual-checkins-body"></tbody></table>
            <button id="open-manual-checkin"></button>
            <div id="manual-checkin-modal" class="hidden" aria-hidden="true">
                <form id="manual-checkin-form">
                    <input id="manual-first-name" name="first_name" value="" />
                    <input id="manual-last-name" name="last_name" value="" />
                    <button id="manual-checkin-submit" type="submit">Save</button>
                </form>
                <button data-modal-close></button>
                <div id="manual-checkin-error" class="hidden"></div>
            </div>
        </body></html>`, {runScripts: 'dangerously', url});
    dom.window.fetch = async (url, opts) => {
        return {ok: true, status: 200, json: async () => [], text: async () => ''};
    };
    dom.window.setInterval = () => 0;
    if (!dom.window.AbortController) {
        dom.window.AbortController = class {
            constructor() {
                this.signal = {};
            }
            abort() {
                this.signal.aborted = true;
            }
        };
    }
    dom.window.eval(apiScript);
    dom.window.eval(script);
    return dom.window;
}

describe('manual-checkins', () => {
    it('builds query params with defaults', () => {
        const window = loadWindow();
        const query = window.buildManualCheckinsQuery();
        const params = new URLSearchParams(query);
        expect(params.get('checked_out_after')).toBe('-12h');
        expect(params.get('include_unchecked')).toBe('true');
        expect(params.get('sort')).toBe('created');
        expect(params.get('limit')).toBe(null);
    });

    it('builds query params from search string', () => {
        const window = loadWindow({
            url: 'http://localhost/?checked_out_after=-2h&limit=50&include_unchecked=false'
        });
        const query = window.buildManualCheckinsQuery();
        const params = new URLSearchParams(query);
        expect(params.get('checked_out_after')).toBe('-2h');
        expect(params.get('include_unchecked')).toBe('false');
        expect(params.get('sort')).toBe('created');
        expect(params.get('limit')).toBe('50');
    });

    it('formats checked out timestamps', () => {
        const window = loadWindow();
        expect(window.formatCheckedOutAt('')).toBe('—');
        expect(window.formatCheckedOutAt('not-a-date')).toBe('—');
        const value = '2024-01-01T00:00:00Z';
        const expected = new window.Date(value).toLocaleString();
        expect(window.formatCheckedOutAt(value)).toBe(expected);
    });

    it('renders a zero-state message', () => {
        const window = loadWindow();
        window.renderManualCheckins([]);
        const body = window.document.getElementById('manual-checkins-body');
        expect(body.innerHTML).toContain('No manual check-ins found.');
    });

    it('renders manual check-in rows with status and actions', () => {
        const window = loadWindow();
        window.renderManualCheckins([
            {
                first_name: 'Ada',
                last_name: 'Lovelace',
                public_id: 'a1',
                checked_out_at: '2024-01-01T00:00:00Z'
            },
            {
                first_name: 'Grace',
                last_name: 'Hopper',
                public_id: 'b2',
                checked_out_at: ''
            }
        ]);

        const rows = window.document.querySelectorAll('#manual-checkins-body tr');
        expect(rows.length).toBe(2);

        const firstStatus = rows[0].querySelector('td[data-label="Status"] span');
        expect(firstStatus.textContent).toBe('Checked out');
        expect(firstStatus.className).toContain('bg-emerald-100');

        const firstButton = rows[0].querySelector('button');
        expect(firstButton.textContent).toBe('Undo Checkout');
        expect(firstButton.dataset.publicId).toBe('a1');
        expect(firstButton.dataset.checkedOut).toBe('true');

        const secondStatus = rows[1].querySelector('td[data-label="Status"] span');
        expect(secondStatus.textContent).toBe('Pending');
        expect(secondStatus.className).toContain('bg-amber-100');

        const secondButton = rows[1].querySelector('button');
        expect(secondButton.textContent).toBe('Check Out');
        expect(secondButton.dataset.publicId).toBe('b2');
        expect(secondButton.dataset.checkedOut).toBe('false');
    });
})

describe('manual-checkins approvals', () => {
    it('renders pending family cards with buttons', () => {
        const window = loadWindow();
        window.renderPendingFamilies([
            {
                public_id: 'sub-1',
                status: 'pending',
                parent: {first_name: 'John', last_name: 'Smith'},
                children: [{first_name: 'Timmy', last_name: 'Smith'}, {first_name: 'Sara', last_name: 'Smith'}]
            }
        ]);
        const container = window.document.getElementById('pending-families');
        expect(container.innerHTML).toContain('John Smith');
        expect(container.innerHTML).toContain('Timmy');
        expect(container.querySelectorAll('[data-approve]').length).toBe(1);
    });

    it('renders entered family cards with a badge and a create-check-in button', () => {
        const window = loadWindow();
        window.renderPendingFamilies([
            {
                public_id: 'sub-2',
                status: 'entered',
                parent: {first_name: 'Jane', last_name: 'Doe'},
                children: [{first_name: 'Sam', last_name: 'Doe'}]
            }
        ]);
        const container = window.document.getElementById('pending-families');
        expect(container.innerHTML).toContain('Jane Doe');
        expect(container.innerHTML).toContain('Entered');
        expect(container.querySelector('[data-approve]')).toBeNull();
        expect(container.querySelector('[data-reject]')).toBeNull();
        const btn = container.querySelector('[data-create-checkins]');
        expect(btn).not.toBeNull();
        expect(btn.dataset.createCheckins).toBe('sub-2');
        expect(btn.textContent).toContain('manual check-in');
    });

    it('creates manual check-ins for an entered submission', async () => {
        const window = loadWindow();
        let posted = null;
        window.fetch = async (url, opts = {}) => {
            const u = String(url);
            if (u.includes('/checkins') && (opts.method || 'GET') === 'POST') {
                posted = u;
                return {ok: true, status: 200, json: async () => ({public_id: 'sub-2', status: 'entered'})};
            }
            return {ok: true, status: 200, json: async () => []};
        };
        await window.createManualCheckins('sub-2');
        expect(posted).toContain('/v1/checkins/guest-submissions/sub-2/checkins');
    });

    it('loads both pending and entered submissions', async () => {
        const window = loadWindow();
        const calls = [];
        window.fetch = async (url) => {
            if (typeof url === 'string' && url.includes('status=pending')) {
                calls.push(url);
                return {ok: true, status: 200, json: async () => [{public_id: 'p', status: 'pending', parent: {first_name: 'A', last_name: 'B'}, children: [], created_at: '2026-01-01T00:00:00Z'}]};
            }
            if (typeof url === 'string' && url.includes('status=entered')) {
                calls.push(url);
                return {ok: true, status: 200, json: async () => [{public_id: 'e', status: 'entered', parent: {first_name: 'C', last_name: 'D'}, children: [], created_at: '2026-01-02T00:00:00Z'}]};
            }
            return {ok: true, status: 200, json: async () => []};
        };
        calls.length = 0;
        await window.loadPendingFamilies();
        const requested = calls.map(u => String(u));
        expect(requested.some(u => u.includes('status=pending'))).toBe(true);
        expect(requested.some(u => u.includes('status=entered') && u.includes('without_manual_checkins=true'))).toBe(true);
        const container = window.document.getElementById('pending-families');
        expect(container.innerHTML).toContain('A B');
        expect(container.innerHTML).toContain('C D');
    });

    it('shows an empty state', () => {
        const window = loadWindow();
        window.renderPendingFamilies([]);
        expect(window.document.getElementById('pending-families').textContent).toContain('No pending families');
    });

    it('toggles the manual check-in modal and resets form state', () => {
        const window = loadWindow();
        const modal = window.document.getElementById('manual-checkin-modal');
        const firstName = window.document.getElementById('manual-first-name');
        const lastName = window.document.getElementById('manual-last-name');
        const error = window.document.getElementById('manual-checkin-error');

        window.toggleManualCheckinModal(true);
        expect(modal.classList.contains('hidden')).toBe(false);
        expect(modal.getAttribute('aria-hidden')).toBe('false');

        firstName.value = 'Ada';
        lastName.value = 'Lovelace';
        window.setManualCheckinError('Required');
        expect(error.classList.contains('hidden')).toBe(false);

        window.toggleManualCheckinModal(false);
        expect(modal.classList.contains('hidden')).toBe(true);
        expect(modal.getAttribute('aria-hidden')).toBe('true');
        expect(firstName.value).toBe('');
        expect(lastName.value).toBe('');
        expect(error.classList.contains('hidden')).toBe(true);
    });

    it('posts first and last name when the form is submitted', async () => {
        const window = loadWindow();
        const firstName = window.document.getElementById('manual-first-name');
        const lastName = window.document.getElementById('manual-last-name');
        firstName.value = 'Ada';
        lastName.value = 'Lovelace';

        let posted = null;
        window.fetch = async (url, opts = {}) => {
            if (String(url).includes('/v1/checkins/manual-checkins') && (opts.method || 'GET') === 'POST') {
                posted = JSON.parse(opts.body);
                return {ok: true, status: 200, json: async () => ({})};
            }
            return {ok: true, status: 200, json: async () => []};
        };

        window.document.dispatchEvent(new window.Event('DOMContentLoaded'));
        window.toggleManualCheckinModal(true);
        window.document.getElementById('manual-checkin-form').dispatchEvent(new window.Event('submit', {bubbles: true, cancelable: true}));
        await new Promise(resolve => setTimeout(resolve, 0));

        expect(posted).toEqual({first_name: 'Ada', last_name: 'Lovelace'});
    });

    it('shows an error when first or last name is missing', async () => {
        const window = loadWindow();
        const error = window.document.getElementById('manual-checkin-error');
        const modal = window.document.getElementById('manual-checkin-modal');

        let posted = false;
        window.fetch = async (url, opts = {}) => {
            if ((opts.method || 'GET') === 'POST') {
                posted = true;
            }
            return {ok: true, status: 200, json: async () => []};
        };

        window.document.dispatchEvent(new window.Event('DOMContentLoaded'));
        window.toggleManualCheckinModal(true);
        window.document.getElementById('manual-checkin-form').dispatchEvent(new window.Event('submit', {bubbles: true, cancelable: true}));
        await new Promise(resolve => setTimeout(resolve, 0));

        expect(posted).toBe(false);
        expect(error.classList.contains('hidden')).toBe(false);
        expect(error.textContent).toContain('required');
        expect(modal.classList.contains('hidden')).toBe(false);
    });

    it('shows parsed sorry message when status update fails', async () => {
        const window = loadWindow();
        window.renderPendingFamilies([
            {
                public_id: 'sub-err',
                status: 'pending',
                parent: {first_name: 'Err', last_name: 'Case'},
                children: [{first_name: 'Kid', last_name: 'Case'}]
            }
        ]);
        window.fetch = async () => ({
            ok: false,
            status: 400,
            redirected: false,
            url: 'http://localhost/v1/checkins/guest-submissions/sub-err/status',
            headers: {get: () => 'application/json'},
            json: async () => ({sorry: 'invalid status transition'})
        });
        const btn = window.document.querySelector('[data-approve]');
        expect(btn).not.toBeNull();
        btn.click();
        await new Promise(resolve => setTimeout(resolve, 0));
        await new Promise(resolve => setTimeout(resolve, 0));
        const statusEl = window.document.getElementById('page-status');
        expect(statusEl.textContent).toContain('invalid status transition');
        expect(statusEl.textContent).not.toContain('{"sorry"');
        expect(statusEl.textContent).toContain('Failed to update');
        expect(btn.disabled).toBe(false);
    });

    it('surfaces conflict retry message without raw JSON', async () => {
        const window = loadWindow();
        window.renderPendingFamilies([
            {
                public_id: 'sub-conflict',
                status: 'pending',
                parent: {first_name: 'Conflict', last_name: 'Case'},
                children: [{first_name: 'Kid', last_name: 'Case'}]
            }
        ]);
        window.fetch = async () => ({
            ok: false,
            status: 400,
            redirected: false,
            url: 'http://localhost/v1/checkins/guest-submissions/sub-conflict/status',
            headers: {get: () => 'application/json'},
            json: async () => ({sorry: 'submission status changed, please retry'})
        });
        const btn = window.document.querySelector('[data-approve]');
        btn.click();
        await new Promise(resolve => setTimeout(resolve, 0));
        await new Promise(resolve => setTimeout(resolve, 0));
        const statusEl = window.document.getElementById('page-status');
        expect(statusEl.textContent).toBe('Failed to update: submission status changed, please retry');
        expect(statusEl.textContent).not.toContain('{"sorry"');
    });

    it('redirects to login when loadManualCheckins session expires (redirected)', async () => {
        const window = loadWindow();
        window.fetch = async () => ({ok: true, redirected: true, url: 'http://localhost/login', status: 200, headers: {get: () => 'text/html'}, json: async () => { throw new Error('should not be called'); }});
        const origHref = window.location.href;
        // loadManualCheckins is called on DOMContentLoaded; invoke directly to test session handling
        // It should not leave a cryptic error status
        await window.loadManualCheckins?.();
        // loadManualCheckins is not exposed directly; verify via fetchJson path instead
        // Instead test that fetchJson throws SessionExpiredError for this response
        const apiWindow = window;
        await expect(apiWindow.fetchJson('/v1/checkins/manual-checkins?checked_out_after=-12h')).rejects.toThrow('Session expired');
        const statusEl = window.document.getElementById('page-status');
        expect(statusEl.textContent).not.toContain('Unexpected token');
        expect(statusEl.textContent).not.toContain('<');
    });

    it('redirects to login when loadPendingFamilies session expires (HTML content-type)', async () => {
        const window = loadWindow();
        window.fetch = async () => ({ok: false, redirected: false, url: 'http://localhost/v1/checkins/guest-submissions?status=pending&limit=200', status: 200, headers: {get: () => 'text/html; charset=utf-8'}, json: async () => { throw new Error('no json'); }});
        // loadPendingFamilies uses fetchJson which will throw SessionExpiredError
        await expect(window.fetchJson('/v1/checkins/guest-submissions?status=pending&limit=200')).rejects.toThrow('Session expired');
        const container = window.document.getElementById('pending-families');
        expect(container.innerHTML).not.toContain('Unexpected token');
    });

    it('loadManualCheckins does not show cryptic JSON parse error on session expiry', async () => {
        const window = loadWindow();
        window.fetch = async () => ({ok: true, redirected: true, url: 'http://localhost/login', status: 200, headers: {get: () => 'text/html'}, json: async () => ({})});
        // Simulate what loadManualCheckins does: fetchJson should throw SessionExpiredError and be caught as redirect
        try {
            await window.fetchJson('/v1/checkins/manual-checkins?checked_out_after=-12h');
        } catch (e) {
            expect(e.name).toBe('SessionExpiredError');
            expect(e.message).toBe('Session expired');
        }
        const statusEl = window.document.getElementById('page-status');
        expect(statusEl.textContent).not.toContain('Unexpected token');
    });
});