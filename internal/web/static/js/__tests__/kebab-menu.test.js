import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const scriptPath = path.resolve(process.cwd(), 'internal/web/static/js/kebab-menu.js');
const script = fs.readFileSync(scriptPath, 'utf8');

function menuHtml() {
    return `<!doctype html>
        <html>
        <body>
            <button id="menu-button" type="button" aria-expanded="false" aria-controls="kebab-menu"></button>
            <div id="kebab-menu" class="hidden">
                <a id="guest-checkin-link" href="/checkin">Guest Check-In</a>
                <a id="login-link" href="/login?next=/">Log In</a>
            </div>
        </body></html>`;
}

function loadWindow(html = menuHtml()) {
    const dom = new JSDOM(html, {
        runScripts: 'dangerously',
        url: 'http://localhost/'
    });
    dom.window.fetch = async () => ({ ok: true, json: async () => ({}) });
    dom.window.eval(script);
    return dom.window;
}

describe('kebab-menu', () => {
    it('does not ship auth-gated routes in static source', () => {
        expect(script).not.toContain('/admin');
        expect(script).not.toContain('/logout');
        expect(script).not.toContain('/api/session');
        expect(script.match(/href=/g) || []).toHaveLength(0);
    });

    it('is a no-op when no kebab menu exists on the page', () => {
        const window = loadWindow('<!doctype html><html><body></body></html>');
        window.setupKebabMenu();
        expect(window.document.getElementById('kebab-menu')).toBeNull();
    });

    it('toggles the menu open/closed on button click and updates aria-expanded', () => {
        const window = loadWindow();
        window.setupKebabMenu();
        const button = window.document.getElementById('menu-button');
        const menu = window.document.getElementById('kebab-menu');

        button.click();
        expect(menu.classList.contains('hidden')).toBe(false);
        expect(button.getAttribute('aria-expanded')).toBe('true');

        button.click();
        expect(menu.classList.contains('hidden')).toBe(true);
        expect(button.getAttribute('aria-expanded')).toBe('false');
    });

    it('closes the menu on Escape', () => {
        const window = loadWindow();
        window.setupKebabMenu();
        const button = window.document.getElementById('menu-button');
        const menu = window.document.getElementById('kebab-menu');

        button.click();
        expect(menu.classList.contains('hidden')).toBe(false);

        window.document.dispatchEvent(new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
        expect(menu.classList.contains('hidden')).toBe(true);
        expect(button.getAttribute('aria-expanded')).toBe('false');
    });

    it('closes the menu on outside click but not on menu-internal click', () => {
        const window = loadWindow();
        window.setupKebabMenu();
        const button = window.document.getElementById('menu-button');
        const menu = window.document.getElementById('kebab-menu');

        button.click();
        expect(menu.classList.contains('hidden')).toBe(false);

        menu.dispatchEvent(new window.MouseEvent('click', { bubbles: true }));
        expect(menu.classList.contains('hidden')).toBe(false);

        window.document.body.dispatchEvent(new window.MouseEvent('click', { bubbles: true }));
        expect(menu.classList.contains('hidden')).toBe(true);
    });

    it('closes the menu when a menu link is clicked', () => {
        const window = loadWindow();
        window.setupKebabMenu();
        const button = window.document.getElementById('menu-button');
        const menu = window.document.getElementById('kebab-menu');
        const guest = window.document.getElementById('guest-checkin-link');

        button.click();
        expect(menu.classList.contains('hidden')).toBe(false);

        guest.addEventListener('click', (event) => event.preventDefault());
        guest.dispatchEvent(new window.MouseEvent('click', { bubbles: true, cancelable: true }));
        expect(menu.classList.contains('hidden')).toBe(true);
    });

    it('does not double-bind when setup runs twice', () => {
        const window = loadWindow();
        window.setupKebabMenu();
        window.setupKebabMenu();

        const button = window.document.getElementById('menu-button');
        const menu = window.document.getElementById('kebab-menu');
        button.click();
        expect(menu.classList.contains('hidden')).toBe(false);
        button.click();
        expect(menu.classList.contains('hidden')).toBe(true);
    });

    it('initKebabMenu wires up the toggle', () => {
        const window = loadWindow();
        window.initKebabMenu();

        const button = window.document.getElementById('menu-button');
        const menu = window.document.getElementById('kebab-menu');
        button.click();
        expect(menu.classList.contains('hidden')).toBe(false);
    });
});