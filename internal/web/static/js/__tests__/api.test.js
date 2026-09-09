import { describe, it, expect, vi } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { JSDOM } from 'jsdom';

const scriptPath = path.resolve(process.cwd(), 'internal/web/static/js/api.js');
const script = fs.readFileSync(scriptPath, 'utf8');

function loadWindow() {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    runScripts: 'dangerously',
    url: 'http://localhost/'
  });
  dom.window.eval(script);
  return dom.window;
}

describe('api', () => {
  describe('fetchJson', () => {
    it('is exported to window', () => {
      const window = loadWindow();
      expect(typeof window.fetchJson).toBe('function');
    });

    it('returns parsed JSON for 2xx responses', async () => {
      const window = loadWindow();
      const mockData = { foo: 'bar' };
      window.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: async () => mockData,
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: () => 'application/json' }
      });

      const result = await window.fetchJson('/api');
      expect(result).toEqual(mockData);
    });

    it('throws SessionExpiredError on redirect to /login', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: true,
        redirected: true,
        url: 'http://localhost/login',
        headers: { get: () => 'text/html' }
      });

      await expect(window.fetchJson('/api')).rejects.toThrow('Session expired');
      try {
        await window.fetchJson('/api');
      } catch (e) {
        expect(e).toBeInstanceOf(window.SessionExpiredError);
      }
    });

    it('throws SessionExpiredError when response URL ends with /login', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: false,
        redirected: false,
        url: 'http://localhost/login',
        headers: { get: () => 'text/html' }
      });

      await expect(window.fetchJson('/api')).rejects.toThrow('Session expired');
    });

    it('throws SessionExpiredError when content-type is HTML', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: false,
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: () => 'text/html; charset=utf-8' }
      });

      await expect(window.fetchJson('/api')).rejects.toThrow('Session expired');
    });

    it('parses data.sorry for error messages', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: false,
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: () => 'application/json' },
        status: 400,
        json: async () => ({ sorry: 'Invalid status transition' })
      });

      await expect(window.fetchJson('/api')).rejects.toThrow('Invalid status transition');
    });

    it('parses data.error for 403 responses', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: false,
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: () => 'application/json' },
        status: 403,
        json: async () => ({ error: 'Forbidden: Insufficient permissions' })
      });

      await expect(window.fetchJson('/api')).rejects.toThrow('Forbidden: Insufficient permissions');
    });

    it('falls back to data.message', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: false,
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: () => 'application/json' },
        status: 500,
        json: async () => ({ message: 'Server error message' })
      });

      await expect(window.fetchJson('/api')).rejects.toThrow('Server error message');
    });

    it('falls back to generic status message when no error body', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: false,
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: () => 'application/json' },
        status: 500,
        json: async () => ({})  // Empty body
      });

      await expect(window.fetchJson('/api')).rejects.toThrow('Request failed with status 500');
    });

    it('passes through fetch options', async () => {
      const window = loadWindow();
      const mockData = { success: true };
      window.fetch = vi.fn().mockResolvedValue({
        ok: true,
        json: async () => mockData,
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: () => 'application/json' }
      });

      const options = {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ foo: 'bar' })
      };

      await window.fetchJson('/api', options);
      expect(window.fetch).toHaveBeenCalledWith('/api', options);
    });

    it('handles non-JSON error responses gracefully', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: false,
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: () => 'text/plain' },
        status: 400,
        json: async () => { throw new Error('Not JSON'); }
      });

      await expect(window.fetchJson('/api')).rejects.toThrow('Request failed with status 400');
    });
  });
});