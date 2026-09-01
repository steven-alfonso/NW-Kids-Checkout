import {describe, it, expect, vi, afterEach} from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import {JSDOM} from 'jsdom';

const scriptPath = path.resolve(process.cwd(), 'internal/web/static/pages/guest-checkin/guest-checkin.js');
const apiScriptPath = path.resolve(process.cwd(), 'internal/web/static/js/api.js');
const script = fs.readFileSync(scriptPath, 'utf8');
const apiScript = fs.readFileSync(apiScriptPath, 'utf8');

function loadWindow() {
    const html = `<!doctype html>
        <html>
        <body>
            <form id="kiosk-form" autocomplete="off">
                <input id="parent-first-name" name="parent_first_name" value="">
                <input id="parent-last-name" name="parent_last_name" value="">
                <input id="parent-phone" name="parent_phone" type="tel" value="">
                <input id="parent-email" name="parent_email" type="email" value="">
                <input id="parent-address1" name="parent_address1" value="">
                <input id="parent-address2" name="parent_address2" value="">
                <input id="parent-city" name="parent_city" value="">
                <input id="parent-state" name="parent_state" value="">
                <input id="parent-zip" name="parent_zip" value="">
                <input id="safety-ack" name="safety_ack" type="checkbox">
                <div id="children-container"></div>
                <button id="add-child" type="button">Add child</button>
                <input id="use-parent-last-name" type="checkbox">
                <span id="use-parent-last-name-toggle-bg"></span>
                <span id="use-parent-last-name-toggle-knob"></span>
                <p id="add-child-hint" class="hidden"></p>
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
    // Sync JSDOM Date with Node's mocked Date (for fake timers).
    dom.window.Date = Date;
    dom.window.eval(apiScript);
    dom.window.eval(script);
    return dom.window;
}


function fillValidParent(window) {
    window.document.getElementById('parent-first-name').value = 'John';
    window.document.getElementById('parent-last-name').value = 'Smith';
    window.document.getElementById('parent-phone').value = '5551234';
    window.document.getElementById('parent-email').value = 'john@example.com';
    window.document.getElementById('parent-address1').value = '123 Main St';
    window.document.getElementById('parent-address2').value = '';
    window.document.getElementById('parent-city').value = 'Seattle';
    window.document.getElementById('parent-state').value = 'WA';
    window.document.getElementById('parent-zip').value = '98101';
    const ack = window.document.getElementById('safety-ack');
    if (ack) ack.checked = true;
}
function fillValidChild(window, rowIdx=0) {
    const rows = window.document.querySelectorAll('.child-row');
    const row = rows[rowIdx];
    if (!row) return;
    const fn = row.querySelector('.child-first-name'); if (fn) fn.value = 'Timmy';
    const ln = row.querySelector('.child-last-name'); if (ln) ln.value = 'Smith';
    const dob = row.querySelector('.child-dob'); if (dob) dob.value = '2020-01-01';
    const gender = row.querySelector('.child-gender'); if (gender) gender.value = 'Boy';
    const rel = row.querySelector('.child-relationship'); if (rel) rel.value = 'Parent';
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
        expect(labelTexts).toEqual(['First name *', 'Last name *', 'Birthdate *', 'Grade *', 'Gender *', 'Relationship to child *', 'Dietary restrictions', 'Special needs']);
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
        expect(window.document.getElementById('add-child-hint').classList.contains('hidden')).toBe(false);
        window.removeChildRow(window.document.querySelector('.child-row'));
        expect(window.document.getElementById('add-child').disabled).toBe(false);
        expect(window.document.getElementById('add-child-hint').classList.contains('hidden')).toBe(true);
    });

    it('builds a payload from the DOM', () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '555-1234';
        window.document.getElementById('parent-email').value = 'john@example.com';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-grade').value = '1st';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';

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
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.getElementById('parent-email').value = 'john@example.com';
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';
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

    it('flags malformed phone and email via validity', () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '12';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.getElementById('parent-email').value = 'john.example';
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';

        expect(window.validateForm()).toBe(false);
        expect(window.document.getElementById('parent-phone').validationMessage).toContain('7 digits');
        expect(window.document.getElementById('parent-email').validity.typeMismatch).toBe(true);

        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.getElementById('parent-email').value = 'john@example.com';
        expect(window.validateForm()).toBe(true);
    });

    it('fails validation when both phone and email are empty', () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.getElementById('parent-email').value = '';
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';

        expect(window.validateForm()).toBe(false);
        expect(window.document.getElementById('parent-phone').validationMessage).toContain('Phone is required');
    });

    it('passes validation with a phone only', () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.getElementById('parent-email').value = '';
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';
        expect(window.validateForm()).toBe(true);
    });

    it('passes validation with an email only', () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.getElementById('parent-email').value = 'john@example.com';
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';
        expect(window.validateForm()).toBe(false);
        expect(window.document.getElementById('parent-phone').validationMessage).toContain('Phone is required');
    });

    it('copies the parent last name to all children when the toggle is on', () => {
        const window = loadWindow();
        window.addChildRow();
        window.addChildRow();
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('use-parent-last-name').checked = true;
        window.handleUseParentLastNameChange();
        const lastNames = Array.from(window.document.querySelectorAll('.child-last-name')).map(input => input.value);
        expect(lastNames).toEqual(['Smith', 'Smith', 'Smith']);
    });

    it('keeps child last names in sync while the toggle is on', () => {
        const window = loadWindow();
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('use-parent-last-name').checked = true;
        window.handleUseParentLastNameChange();
        window.document.querySelector('.child-last-name').value = 'Jones';
        window.document.getElementById('parent-last-name').value = 'Johnson';
        window.handleParentLastNameInput();
        const lastNames = Array.from(window.document.querySelectorAll('.child-last-name')).map(input => input.value);
        expect(lastNames).toEqual(['Johnson']);
    });

    it('applies the parent last name to newly added children while the toggle is on', () => {
        const window = loadWindow();
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('use-parent-last-name').checked = true;
        window.handleUseParentLastNameChange();
        window.addChildRow();
        const lastNames = Array.from(window.document.querySelectorAll('.child-last-name')).map(input => input.value);
        expect(lastNames).toEqual(['Smith', 'Smith']);
    });

    it('clears child last names when parent last name is cleared while toggle is on', () => {
        const window = loadWindow();
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('use-parent-last-name').checked = true;
        window.handleUseParentLastNameChange();
        expect(window.document.querySelector('.child-last-name').value).toBe('Smith');

        window.document.getElementById('parent-last-name').value = '';
        window.handleParentLastNameInput();
        expect(window.document.querySelector('.child-last-name').value).toBe('');
    });

    it('toggles the switch visual when the checkbox changes', () => {
        const window = loadWindow();
        const bg = window.document.getElementById('use-parent-last-name-toggle-bg');
        const knob = window.document.getElementById('use-parent-last-name-toggle-knob');
        expect(bg.style.backgroundColor).toBe('var(--color-slate-200)');
        expect(knob.style.transform).toBe('translateX(0)');

        window.document.getElementById('use-parent-last-name').checked = true;
        window.handleUseParentLastNameChange();
        expect(bg.style.backgroundColor).toBe('var(--color-emerald-500)');
        expect(knob.style.transform).toBe('translateX(1rem)');

        window.document.getElementById('use-parent-last-name').checked = false;
        window.handleUseParentLastNameChange();
        expect(bg.style.backgroundColor).toBe('var(--color-slate-200)');
        expect(knob.style.transform).toBe('translateX(0)');
    });

    it('passes validation with a fully valid form', () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.getElementById('parent-email').value = 'john@example.com';
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';
        expect(window.validateForm()).toBe(true);
    });

    it('fails validation when child first name is empty', () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.querySelector('.child-first-name').value = '';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';
        expect(window.validateForm()).toBe(false);
        expect(window.document.querySelector('.child-first-name').validationMessage).toContain('First name is required');
    });

    it('fails validation when child last name is empty', () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = '';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';
        expect(window.validateForm()).toBe(false);
        expect(window.document.querySelector('.child-last-name').validationMessage).toContain('Last name is required');
    });

    it('shows kiosk error when no children are present', () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        const rows = window.document.querySelectorAll('.child-row');
        for (let i = rows.length - 1; i >= 0; i--) {
            rows[i].remove();
        }
        expect(window.validateForm()).toBe(false);
        expect(window.document.getElementById('kiosk-error').textContent).toContain('At least one child');
    });

    it('validates each child row independently', () => {
        const window = loadWindow();
        window.addChildRow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        const rows = window.document.querySelectorAll('.child-row');
        rows[0].querySelector('.child-first-name').value = 'Timmy';
        rows[0].querySelector('.child-last-name').value = 'Smith';
        rows[0].querySelector('.child-dob').value = '2020-01-01';
        rows[0].querySelector('.child-gender').value = 'Boy';
        rows[0].querySelector('.child-relationship').value = 'Parent';
        rows[1].querySelector('.child-first-name').value = '';
        rows[1].querySelector('.child-last-name').value = 'Smith';
        rows[1].querySelector('.child-dob').value = '2020-01-01';
        rows[1].querySelector('.child-gender').value = 'Boy';
        rows[1].querySelector('.child-relationship').value = 'Parent';
        expect(window.validateForm()).toBe(false);
        expect(rows[1].querySelector('.child-first-name').validationMessage).toContain('First name is required');
    });

    it('sets DOB max to local date (en-CA) not UTC', () => {
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2026-03-15T12:00:00Z'));
        const window = loadWindow();
        const expected = new Date().toLocaleDateString('en-CA');
        const dobInput = window.document.querySelector('.child-dob');
        expect(dobInput.getAttribute('max')).toBe(expected);
        // newly added rows also use local date
        window.addChildRow();
        const rows = window.document.querySelectorAll('.child-row');
        expect(rows[1].querySelector('.child-dob').getAttribute('max')).toBe(expected);
        // ensure implementation uses local date, not UTC (en-CA vs toISOString)
        expect(script).not.toContain("toISOString().split('T')[0]");
        expect(script).toContain("toLocaleDateString('en-CA')");
    });

    it('resets toggle visual after resetForm', () => {
        const window = loadWindow();
        const toggle = window.document.getElementById('use-parent-last-name');
        const bg = window.document.getElementById('use-parent-last-name-toggle-bg');
        const knob = window.document.getElementById('use-parent-last-name-toggle-knob');
        toggle.checked = true;
        window.handleUseParentLastNameChange();
        expect(bg.style.backgroundColor).toBe('var(--color-emerald-500)');
        expect(knob.style.transform).toBe('translateX(1rem)');
        window.resetForm();
        expect(toggle.checked).toBe(false);
        expect(bg.style.backgroundColor).toBe('var(--color-slate-200)');
        expect(knob.style.transform).toBe('translateX(0)');
    });

    it('rejects a future DOB in validateForm', () => {
        vi.useFakeTimers();
        vi.setSystemTime(new Date('2026-03-15T12:00:00Z'));
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        // use a clearly future date regardless of mocked today
        const future = '2099-01-01';
        window.document.querySelector('.child-dob').value = future;
        expect(window.validateForm()).toBe(false);
        expect(window.document.querySelector('.child-dob').validationMessage).toContain('Birthdate cannot be in the future');
        // past date should pass (phone required, etc. already satisfied)
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';
        expect(window.validateForm()).toBe(true);
    });

    it('shows friendly sorry message on non-2xx and keeps form data', async () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';
        window.fetch = vi.fn().mockResolvedValue({
            ok: false,
            status: 400,
            redirected: false,
            url: 'http://localhost/v1/checkins/guest-submissions',
            headers: { get: () => 'application/json' },
            json: async () => ({ sorry: 'invalid status transition' })
        });
        await window.submitKioskForm();
        const errorEl = window.document.getElementById('kiosk-error');
        expect(errorEl.textContent).toBe('invalid status transition');
        expect(errorEl.classList.contains('hidden')).toBe(false);
        // form data preserved, not reset, welcome panel stays hidden
        expect(window.document.getElementById('parent-first-name').value).toBe('John');
        expect(window.document.getElementById('welcome-panel').classList.contains('hidden')).toBe(true);
        // button re-enabled
        expect(window.document.getElementById('kiosk-submit').disabled).toBe(false);
    });

    it('shows session-expired message and preserves form on HTML redirect', async () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';
        window.fetch = vi.fn().mockResolvedValue({
            ok: true,
            status: 200,
            redirected: true,
            url: 'http://localhost/login',
            headers: { get: () => 'text/html' },
            json: async () => ({})
        });
        await window.submitKioskForm();
        const errorEl = window.document.getElementById('kiosk-error');
        expect(errorEl.textContent).toBe('Please ask a staff member to sign in');
        expect(window.document.getElementById('parent-first-name').value).toBe('John');
        expect(window.document.getElementById('welcome-panel').classList.contains('hidden')).toBe(true);
        expect(window.document.getElementById('kiosk-submit').disabled).toBe(false);
    });

    it('shows error on network rejection and re-enables button', async () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';
        window.fetch = vi.fn().mockRejectedValue(new Error('Network error'));
        await window.submitKioskForm();
        const errorEl = window.document.getElementById('kiosk-error');
        expect(errorEl.textContent).toBe('Network error');
        expect(window.document.getElementById('kiosk-submit').disabled).toBe(false);
        expect(window.document.getElementById('parent-first-name').value).toBe('John');
    });

    it('prevents double-submit while request is in flight', async () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';
        let resolveFetch;
        const fetchPromise = new Promise(resolve => { resolveFetch = resolve; });
        window.fetch = vi.fn().mockReturnValue(fetchPromise);
        const first = window.submitKioskForm();
        // button should be disabled during flight
        expect(window.document.getElementById('kiosk-submit').disabled).toBe(true);
        // second submit should be no-op
        const second = window.submitKioskForm();
        expect(window.fetch).toHaveBeenCalledTimes(1);
        await second;
        // resolve first fetch as success
        resolveFetch({
            ok: true,
            redirected: false,
            url: 'http://localhost/v1/checkins/guest-submissions',
            headers: { get: () => 'application/json' },
            json: async () => ({ public_id: 'abc' })
        });
        await first;
        expect(window.document.getElementById('welcome-panel').classList.contains('hidden')).toBe(false);
        expect(window.document.getElementById('kiosk-submit').disabled).toBe(false);
    });

    it('re-enables button in finally after failure', async () => {
        const window = loadWindow();
        window.document.getElementById('parent-first-name').value = 'John';
        window.document.getElementById('parent-last-name').value = 'Smith';
        window.document.getElementById('parent-phone').value = '5551234';
        window.document.getElementById('parent-address1').value = '123 Main St';
        window.document.getElementById('parent-city').value = 'Seattle';
        window.document.getElementById('parent-state').value = 'WA';
        window.document.getElementById('parent-zip').value = '98101';
        window.document.getElementById('safety-ack').checked = true;
        window.document.querySelector('.child-first-name').value = 'Timmy';
        window.document.querySelector('.child-last-name').value = 'Smith';
        window.document.querySelector('.child-dob').value = '2020-01-01';
        window.document.querySelector('.child-gender').value = 'Boy';
        window.document.querySelector('.child-relationship').value = 'Parent';
        window.fetch = vi.fn().mockResolvedValue({
            ok: false,
            status: 500,
            redirected: false,
            url: 'http://localhost/v1/checkins/guest-submissions',
            headers: { get: () => 'application/json' },
            json: async () => ({ sorry: 'server error' })
        });
        expect(window.document.getElementById('kiosk-submit').disabled).toBe(false);
        await window.submitKioskForm();
        expect(window.document.getElementById('kiosk-submit').disabled).toBe(false);
        expect(window.document.getElementById('kiosk-error').textContent).toBe('server error');
    });
});