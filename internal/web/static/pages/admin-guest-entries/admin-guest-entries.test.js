import {describe, it, expect} from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import {JSDOM} from 'jsdom';

const scriptPath = path.resolve(process.cwd(), 'internal/web/static/pages/admin-guest-entries/admin-guest-entries.js');
const script = fs.readFileSync(scriptPath, 'utf8');

function loadWindow() {
    const dom = new JSDOM(`<!doctype html>
        <html>
        <body>
            <div id="entries-container"></div>
            <div id="page-status" class="hidden"></div>
        </body></html>`, {runScripts: 'dangerously', url: 'http://localhost/'});
    dom.window.fetch = async (url, opts) => ({ok: true, status: 200, json: async () => []});
    dom.window.navigator.clipboard = {writeText: async (text) => (dom.window._copied = text)};
    dom.window.eval(script);
    return dom.window;
}

describe('admin-guest-entries', () => {
    it('groups submissions by status, approved first', () => {
        const window = loadWindow();
        const sub = (publicId, status) => ({public_id: publicId, status});
        const groups = window.groupByStatus([
            sub('c', 'entered'),
            sub('a', 'approved'),
            sub('b', 'pending'),
            sub('d', 'rejected')
        ]);
        expect(groups[0][0]).toEqual('approved');
        expect(groups[1][0]).toEqual('pending');
        expect(groups[2][0]).toEqual('entered');
        expect(groups[3][0]).toEqual('rejected');
    });

    it('renders each field as a labeled click-to-copy chip', () => {
        const window = loadWindow();
        const entry = {
            public_id: 'sub-1',
            status: 'approved',
            parent: {first_name: 'John', last_name: 'Smith', phone: '555-1234', email: 'j@e.com'},
            children: [{first_name: 'Timmy', last_name: 'Smith', dob: '2020-01-01', grade: '1st Grade'}]
        };
        const container = window.document.createElement('div');
        const chips = window.renderEntry(container, entry);
        expect(container.querySelectorAll('[data-copy]').length).toBeGreaterThanOrEqual(8);
        expect(chips[0].dataset.copy).toBe('John');

        const labels = Array.from(container.querySelectorAll('span.text-xs')).map(el => el.textContent);
        expect(labels).toContain('first name');
        expect(labels).toContain('email');
        expect(labels).toContain('grade');
    });

    it('renders missing phone/email as an em dash on parent chips', () => {
        const window = loadWindow();
        const entry = {
            public_id: 'sub-4',
            status: 'approved',
            parent: {first_name: 'A', last_name: 'B', phone: '', email: ''},
            children: []
        };
        const container = window.document.createElement('div');
        window.renderEntry(container, entry);
        const phoneChip = Array.from(container.querySelectorAll('[data-copy]')).find(btn => btn.dataset.label === 'phone');
        const emailChip = Array.from(container.querySelectorAll('[data-copy]')).find(btn => btn.dataset.label === 'email');
        expect(phoneChip.textContent).toBe('—');
        expect(emailChip.textContent).toBe('—');
    });

    it('shows a Copied tooltip on the field after copying', () => {
        const window = loadWindow();
        const entry = {
            public_id: 'sub-3',
            status: 'approved',
            parent: {first_name: 'A', last_name: 'B', phone: '555', email: 'a@b.com'},
            children: []
        };
        const container = window.document.createElement('div');
        window.renderEntry(container, entry);
        const chipBtn = container.querySelector('[data-copy]');
        const field = chipBtn.closest('.relative');
        window.showCopiedTooltip(field);

        const tip = field.querySelector('.copy-tooltip');
        expect(tip).not.toBeNull();
        expect(tip.textContent).toBe('Copied');
    });

    it('copies a single value', async () => {
        const window = loadWindow();
        await window.copyValue('Timmy');
        expect(window._copied).toBe('Timmy');
    });

    it('uses distinct badge colors per status', () => {
        const window = loadWindow();
        const entry = {
            public_id: 'sub-2',
            status: 'approved',
            parent: {first_name: 'A', last_name: 'B', phone: '555', email: 'a@b.com'},
            children: []
        };
        const container = window.document.createElement('div');
        window.renderEntry(container, entry);
        const badge = container.querySelector('.rounded-full');
        expect(badge.className).toContain('bg-amber-100');
        expect(badge.className).toContain('text-amber-800');
    });

    it('adds an amber accent to approved cards', () => {
        const window = loadWindow();
        const container = window.document.createElement('div');
        window.renderEntry(container, {
            public_id: 'appr-1',
            status: 'approved',
            parent: {first_name: 'A', last_name: 'B', phone: '1', email: 'a@b.com'},
            children: []
        });
        window.renderEntry(container, {
            public_id: 'pend-1',
            status: 'pending',
            parent: {first_name: 'C', last_name: 'D', phone: '2', email: 'c@d.com'},
            children: []
        });
        const cards = container.querySelectorAll('.rounded-xl');
        const approvedCard = cards[0];
        const pendingCard = cards[1];
        expect(approvedCard.className).toContain('border-l-amber-400');
        expect(pendingCard.className).not.toContain('border-l-amber-400');
    });

    it('renders the mark-entered button on approved cards', () => {
        const window = loadWindow();
        const container = window.document.createElement('div');
        window.renderEntry(container, {
            public_id: 'appr-1',
            status: 'approved',
            parent: {first_name: 'A', last_name: 'B', phone: '1', email: 'a@b.com'},
            children: []
        });
        const btn = container.querySelector('[data-mark-entered="appr-1"]');
        expect(btn).not.toBeNull();
    });

    it('does not render the mark-entered button on pending cards', () => {
        const window = loadWindow();
        const container = window.document.createElement('div');
        window.renderEntry(container, {
            public_id: 'pend-1',
            status: 'pending',
            parent: {first_name: 'C', last_name: 'D', phone: '2', email: 'c@d.com'},
            children: []
        });
        const btn = container.querySelector('[data-mark-entered="pend-1"]');
        expect(btn).toBeNull();
    });

    it('renders the entered section collapsed by default and toggles it open', async () => {
        const window = loadWindow();
        const entered = {public_id: 'e-1', status: 'entered', parent: {first_name: 'E', last_name: 'F', phone: '3', email: 'e@f.com'}, children: []};
        window.fetch = async () => ({ok: true, status: 200, json: async () => [entered]});
        await window.loadEntries();

        const getBody = () => window.document.getElementById('collapsible-section-entered');
        const getToggle = () => window.document.querySelector('[data-collapse-toggle="entered"]');

        expect(getBody()).not.toBeNull();
        expect(getBody().classList.contains('hidden')).toBe(true);
        expect(getToggle().textContent).toContain('▸');
        expect(getToggle().getAttribute('aria-expanded')).toBe('false');

        getToggle().click();
        expect(getBody().classList.contains('hidden')).toBe(false);
        expect(getBody().classList.contains('space-y-4')).toBe(true);
        expect(getToggle().getAttribute('aria-expanded')).toBe('true');
        expect(getToggle().textContent).toContain('▾');

        getToggle().click();
        expect(getBody().classList.contains('hidden')).toBe(true);
        expect(getBody().classList.contains('space-y-4')).toBe(false);
    });
});