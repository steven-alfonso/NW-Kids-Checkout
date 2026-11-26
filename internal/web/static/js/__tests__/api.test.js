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
  describe('fetchJsonWithETag', () => {
    it('is exported to window', () => {
      const window = loadWindow();
      expect(typeof window.fetchJsonWithETag).toBe('function');
    });

    it('returns data and the ETag for a 200 response', async () => {
      const window = loadWindow();
      const mockData = { foo: 'bar' };
      window.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => mockData,
        clone: () => ({ text: async () => JSON.stringify(mockData) }),
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: (name) => (name === 'ETag' ? 'W/"v1"' : 'application/json') }
      });

      const result = await window.fetchJsonWithETag('/api');
      expect(result).toEqual({ notModified: false, etag: 'W/"v1"', data: mockData });
    });

    it('returns notModified without parsing a 304 response', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 304,
        json: async () => {
          throw new Error('304 must not be parsed');
        },
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: (name) => (name === 'ETag' ? 'W/"v1"' : null) }
      });

      const result = await window.fetchJsonWithETag('/api');
      expect(result).toEqual({ notModified: true, etag: 'W/"v1"', data: null });
    });

    it('maps a 401 JSON response to SessionExpiredError', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 401,
        redirected: false,
        url: 'http://localhost/v1/checkins/checkouts',
        headers: { get: () => 'application/json' }
      });

      await expect(window.fetchJsonWithETag('/api')).rejects.toThrow('Session expired');
    });

    it('does not treat a non-login HTML error page as session expiry', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 503,
        redirected: false,
        url: 'http://localhost/v1/checkins/checkouts',
        headers: { get: () => 'text/html' },
        json: async () => {
          throw new Error('not JSON');
        }
      });

      await expect(window.fetchJsonWithETag('/api')).rejects.toThrow(
        'Request failed with status 503'
      );
    });

    it('does not treat an empty 5xx as unchanged', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 503,
        redirected: false,
        url: 'http://localhost/v1/checkins/checkouts',
        headers: { get: () => null },
        clone: () => ({ text: async () => '' }),
        json: async () => {
          throw new Error('not JSON');
        }
      });

      await expect(
        window.fetchJsonWithETag('/api', { headers: { 'If-None-Match': 'W/"old"' } })
      ).rejects.toThrow('Request failed with status 503');
    });

    it('returns an empty validator when a 200 has no ETag', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: () => null },
        json: async () => ({ ok: true })
      });

      const result = await window.fetchJsonWithETag('/api');
      expect(result).toEqual({ notModified: false, etag: '', data: { ok: true } });
    });

    it('keeps the sent validator when an intermediary returns an empty 200', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: (name) => (name === 'ETag' ? 'W/"new"' : 'application/json') },
        clone: () => ({ text: async () => '' }),
        json: async () => {
          throw new Error('an empty 200 must not be parsed');
        }
      });

      const result = await window.fetchJsonWithETag('/api', {
        headers: { 'If-None-Match': 'W/"old"' }
      });

      expect(result).toEqual({ notModified: true, etag: 'W/"old"', data: null });
    });

    it('retries without the validator when a 304 carries a different ETag', async () => {
      const window = loadWindow();
      const fetchMock = vi.fn()
        .mockResolvedValueOnce({
          status: 304,
          headers: { get: (name) => (name === 'ETag' ? 'W/"other"' : null) }
        })
        .mockResolvedValueOnce({
          ok: true,
          status: 200,
          redirected: false,
          url: 'http://localhost/api',
          headers: { get: (name) => (name === 'ETag' ? 'W/"good"' : 'application/json') },
          json: async () => ({ ok: true })
        });
      window.fetch = fetchMock;

      const result = await window.fetchJsonWithETag('/api', {
        headers: { 'If-None-Match': 'W/"old"' }
      });

      expect(fetchMock).toHaveBeenCalledTimes(2);
      expect(fetchMock.mock.calls[1][1].headers).not.toHaveProperty('If-None-Match');
      expect(result).toEqual({ notModified: false, etag: 'W/"good"', data: { ok: true } });
    });
  });

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

    it('reports a non-login HTML error page as a status error', async () => {
      const window = loadWindow();
      window.fetch = vi.fn().mockResolvedValue({
        ok: false,
        status: 503,
        redirected: false,
        url: 'http://localhost/api',
        headers: { get: () => 'text/html; charset=utf-8' },
        json: async () => {
          throw new Error('not JSON');
        }
      });

      await expect(window.fetchJson('/api')).rejects.toThrow(
        'Request failed with status 503'
      );
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