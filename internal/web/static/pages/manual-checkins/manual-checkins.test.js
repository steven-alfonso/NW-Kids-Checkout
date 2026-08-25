import {describe, it, expect} from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import {JSDOM} from 'jsdom';

const scriptPath = path.resolve(process.cwd(), 'internal/web/static/pages/manual-checkins/manual-checkins.js');
const script = fs.readFileSync(scriptPath, 'utf8');

function loadWindow() {
    const dom = new JSDOM(`<!doctype html>
        <html>
        <body>
            <div id="page-status" class="hidden"></div>
            <div id="pending-families"></div>
            <table><tbody id="manual-checkins-body"></tbody></table>
        </body></html>`, {runScripts: 'dangerously', url: 'http://localhost/'});
    dom.window.fetch = async (url, opts) => {
        if (typeof url === 'string' && url.includes('/guest-submissions?status=pending')) {
            return {ok: true, status: 200, json: async () => []};
        }
        return {ok: true, status: 200, json: async () => []};
    };
    dom.window.setInterval = () => 0;
    dom.window.eval(script);
    return dom.window;
}

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

    it('shows an empty state', () => {
        const window = loadWindow();
        window.renderPendingFamilies([]);
        expect(window.document.getElementById('pending-families').textContent).toContain('No pending families');
    });
});