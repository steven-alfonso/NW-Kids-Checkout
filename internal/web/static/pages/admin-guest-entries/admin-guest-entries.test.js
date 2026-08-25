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

    it('renders each field as a click-to-copy chip', () => {
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
    });

    it('copies a single value', async () => {
        const window = loadWindow();
        await window.copyValue('Timmy');
        expect(window._copied).toBe('Timmy');
    });
});