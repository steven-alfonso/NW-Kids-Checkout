// Static server for the checkouts conformance suite.
//
// Serves the real embedded-asset tree so the spec exercises the same HTML,
// scripts, CSS, and icons the Go binary ships. `/v1/*` is deliberately NOT
// implemented here: the spec mocks every API call with page.route, and a
// readable 503 makes an unhandled request obvious instead of looking like a
// normal empty response.
import { createServer } from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { extname, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(fileURLToPath(new URL('../../internal/web/static', import.meta.url)));
const PORT = 4173;
const HOST = '127.0.0.1';

const MIME_TYPES = {
  '.avif': 'image/avif',
  '.css': 'text/css; charset=utf-8',
  '.html': 'text/html; charset=utf-8',
  '.ico': 'image/x-icon',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.map': 'application/json; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
  '.webp': 'image/webp',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2'
};

function send(res, status, contentType, body) {
  res.writeHead(status, {
    'Cache-Control': 'no-store',
    'Content-Type': contentType,
    'Content-Length': Buffer.byteLength(body)
  });
  res.end(body);
}

function sendJSON(res, status, value) {
  send(res, status, 'application/json; charset=utf-8', JSON.stringify(value));
}

const server = createServer(async (req, res) => {
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    sendJSON(res, 405, { error: `unsupported method ${req.method}` });
    return;
  }

  let pathname;
  try {
    pathname = decodeURIComponent(new URL(req.url, `http://${HOST}:${PORT}`).pathname);
  } catch {
    sendJSON(res, 400, { error: 'invalid URL' });
    return;
  }

  if (pathname === '/health') {
    send(res, 200, 'text/plain; charset=utf-8', 'ok');
    return;
  }

  if (pathname === '/') {
    res.writeHead(302, { Location: '/static/pages/checkoutsv1/checkouts.html' });
    res.end();
    return;
  }

  if (pathname.startsWith('/v1/')) {
    sendJSON(res, 503, {
      error: `unhandled conformance API request: ${pathname}`,
      hint: 'add a page.route mock in test-support/conformance/fake-api.js'
    });
    return;
  }

  if (!pathname.startsWith('/static/')) {
    sendJSON(res, 404, { error: `not found: ${pathname}` });
    return;
  }

  const relative = pathname.slice('/static/'.length);
  const filePath = resolve(ROOT, relative);
  if (filePath !== ROOT && !filePath.startsWith(`${ROOT}${sep}`)) {
    sendJSON(res, 403, { error: 'path traversal blocked' });
    return;
  }

  try {
    const info = await stat(filePath);
    if (!info.isFile()) {
      sendJSON(res, 404, { error: `not a file: ${pathname}` });
      return;
    }
    const body = await readFile(filePath);
    const contentType = MIME_TYPES[extname(filePath).toLowerCase()] || 'application/octet-stream';
    res.writeHead(200, {
      'Cache-Control': 'no-store',
      'Content-Type': contentType,
      'Content-Length': body.length
    });
    res.end(req.method === 'HEAD' ? undefined : body);
  } catch (error) {
    if (error?.code === 'ENOENT') {
      sendJSON(res, 404, { error: `not found: ${pathname}` });
      return;
    }
    sendJSON(res, 500, { error: `could not read ${pathname}: ${error.message}` });
  }
});

server.listen(PORT, HOST, () => {
  console.log(`checkouts conformance server listening on http://${HOST}:${PORT}`);
});
