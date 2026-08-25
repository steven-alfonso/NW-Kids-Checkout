import {describe, it, expect, vi, afterEach} from 'vitest';
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
                <input id="parent-phone" name="parent_phone" type="tel" pattern=".*\\d.*\\d.*\\d.*\\d.*\\d.*\\d.*\\d.*" value="">
                <input id="parent-email" name="parent_email" type="email" value="">
                <div id="children-container"></div>
                <button id="add-child" type="button">Add child</button>
                <button id="kiosk-submit" type="submit">Submit</button>
                <div id="kiosk-error" class="hidden"></div>
            </form>
            <div id="welcome-panel" class="hidden">
                <button id="new-submission-button" type="button">Create new submission</button>
                <p id="countdown-text"></p>
            </div>
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

afterEach(() => {
    vi.useRealTimers();
});

describe('kiosk form', () => {
    it('renders an initial child row', () => {
        const window = loadWindow();
        expect(window.document.querySelectorAll('.child-row').length).toBe(1);
    });

    it('labels every child field', () => {
        const window = loadWindow();
        const labels = Array.from(window.document.querySelectorAll('.child-row label'));
        const labelTexts = labels.map(label => {
        const clone = label.cloneNode(true);
        clone.querySelectorAll('input, select').forEach(el => el.remove());
        return clone.textContent.replace(/\s+/g, ' ').trim();
    });
        expect(labelTexts).toEqual(['First name', 'Last name', 'Birthdate', 'Grade']);
    });

    it('renders the grade dropdown with expected options', () => {
        const window = loadWindow();
        const select = window.document.querySelector('.child-grade');
        expect(select).not.toBeNull();
        const options = Array.from(select.options).map(option => option.value);
        expect(options).toEqual(['None', 'Pre-K', 'Kindergarten', '1st', '2nd', '3rd', '4th', '5th', '6th', '7th', '8th', '9th', '10th', '11th', '12th']);
        expect(select.value).toBe('None');
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

    it('caps child rows at 10 and disables the add button', () => {
        const window = loadWindow();
        for (let i = 0; i < 12; i++) {
            window.addChildRow();
        }
        expect(window.document.querySelectorAll('.child-row').length).toBe(10);
        expect(window.document.getElementById('add-child').disabled).toBe(true);
        window.removeChildRow(window.document.querySelector('.child-row'));
        expect(window.document.getElementById('add-child').disabled).toBe(false);
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
        window.document.querySelector('.child-grade').value = '1st';

        const payload = window.buildPayload();
        expect(payload.parent.first_name).toBe('John');
        expect(payload.children).toHaveLength(1);
        expect(payload.children[0].grade).toBe('1st');
    });

    it('resets the form and shows welcome on submit', async () => {
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2026-02-01T00:00:00Z'));
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-email').value = 'john@example.com';
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        await window.submitKioskForm();
        expect(window.document.getElementById('welcome-panel').classList.contains('hidden')).toBe(false);
        expect(window.document.getElementById('parent-first-name').value).toBe('');
        expect(window.document.getElementById('countdown-text').textContent).toContain('10');
        window.stopCountdown();
    });

    it('counts down and returns to the form after 10 seconds', () => {
        vi.useFakeTimers();
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.showWelcome();

        expect(window.document.getElementById('countdown-text').textContent).toContain('10');

        vi.advanceTimersByTime(9000);
        expect(window.document.getElementById('countdown-text').textContent).toContain('1');
        expect(window.document.getElementById('welcome-panel').classList.contains('hidden')).toBe(false);

        vi.advanceTimersByTime(1000);
        expect(window.document.getElementById('welcome-panel').classList.contains('hidden')).toBe(true);
        expect(window.document.getElementById('kiosk-form').classList.contains('hidden')).toBe(false);
        expect(window.document.getElementById('parent-first-name').value).toBe('');
    });

    it('create new submission button shows the form immediately', () => {
        vi.useFakeTimers();
        const window = loadWindow();
        window.showWelcome();
        expect(window.document.getElementById('welcome-panel').classList.contains('hidden')).toBe(false);

        window.document.getElementById('new-submission-button').click();
        expect(window.document.getElementById('welcome-panel').classList.contains('hidden')).toBe(true);
        expect(window.document.getElementById('kiosk-form').classList.contains('hidden')).toBe(false);

        vi.advanceTimersByTime(15000);
        expect(window.document.getElementById('welcome-panel').classList.contains('hidden')).toBe(true);
    });

    it('does not submit when required fields are missing', () => {
        const window = loadWindow();
        const submitSpy = vi.spyOn(window, 'postSubmission');
        window.submitKioskForm();
        expect(submitSpy).not.toHaveBeenCalled();
        expect(window.validateForm()).toBe(false);
        expect(window.document.getElementById('kiosk-error').classList.contains('hidden')).toBe(true);
    });

    it('flags malformed phone and email via native validity', () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '12';
        window.document.getElementById('parent-email').value = 'john.example';
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';

        expect(window.validateForm()).toBe(false);
        expect(window.document.getElementById('parent-phone').validity.patternMismatch).toBe(true);
        expect(window.document.getElementById('parent-email').validity.typeMismatch).toBe(true);

        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-email').value = 'john@example.com';
        expect(window.validateForm()).toBe(true);
    });

    it('passes validation with a fully valid form', () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-email').value = 'john@example.com';
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        expect(window.validateForm()).toBe(true);
    });
});