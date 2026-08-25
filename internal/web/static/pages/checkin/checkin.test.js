import {describe, it, expect} from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import {JSDOM} from 'jsdom';

const scriptPath = path.resolve(process.cwd(), 'internal/web/static/pages/checkin/checkin.js');
const script = fs.readFileSync(scriptPath, 'utf8');

function loadWindow() {
    const html = `<!doctype html>
        <html>
        <body>
            <form id="kiosk-form" autocomplete="off">
                <input id="parent-first-name" name="parent_first_name" value="">
                <input id="parent-last-name" name="parent_last_name" value="">
                <input id="parent-phone" name="parent_phone" value="">
                <input id="parent-email" name="parent_email" value="">
                <div id="children-container"></div>
                <button id="add-child" type="button">Add child</button>
                <button id="kiosk-submit" type="submit">Submit</button>
                <div id="kiosk-error" class="hidden"></div>
            </form>
            <div id="welcome-panel" class="hidden"></div>
        </body></html>`;
    const dom = new JSDOM(html, {runScripts: 'dangerously', url: 'http://localhost/'});
    dom.window.fetch = async (url, opts) => ({
        ok: true,
        status: 200,
        json: async () => ({public_id: 'abc', status: 'pending'}),
        text: async () => ''
    });
    dom.window.eval(script);
    return dom.window;
}

describe('kiosk form', () => {
    it('renders an initial child row', () => {
        const window = loadWindow();
        expect(window.document.querySelectorAll('.child-row').length).toBe(1);
    });

    it('adds and removes child rows', () => {
        const window = loadWindow();
        window.addChildRow();
        window.addChildRow();
        expect(window.document.querySelectorAll('.child-row').length).toBe(3);
        const first = window.document.querySelector('.child-row');
        window.removeChildRow(first);
        expect(window.document.querySelectorAll('.child-row').length).toBe(2);
    });

    it('builds a payload from the DOM', () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '555-1234';
        window.document.getElementById('parent-email').value = 'john@example.com';
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-grade').value = '1st Grade';

        const payload = window.buildPayload();
        expect(payload.parent.first_name).toBe('John');
        expect(payload.children).toHaveLength(1);
        expect(payload.children[0].grade).toBe('1st Grade');
    });

    it('resets the form and shows welcome on submit', async () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        await window.submitKioskForm();
        expect(window.document.getElementById('welcome-panel').classList.contains('hidden')).toBe(false);
        expect(window.document.getElementById('parent-first-name').value).toBe('');
    });
});