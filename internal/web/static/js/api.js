// Shared fetch wrapper for handling session expiry and error envelopes
// Matches the pattern used by kebab-menu.js for script tag inclusion and consumption

/**
 * Custom error for session expiration
 */
class SessionExpiredError extends Error {
  constructor(message) {
    super(message);
    this.name = 'SessionExpiredError';
  }
}

/**
 * Validate a response for session expiry or an error envelope, then parse JSON.
 * @param {Response} response
 * @returns {Promise<Object>} Parsed JSON response
 */
async function readJsonResponse(response) {
  const responseUrl = response.url || '';
  const contentType = response.headers ? (response.headers.get('content-type') || '') : '';
  const isHtml = contentType.includes('text/html');
  const isServerError = response.status >= 500;

  // A login URL or 401 means the session expired. An HTML response below 500
  // is the login page served without a redirect; a 5xx HTML page is an outage.
  if (/\/login(?:\/|\?|$)/.test(responseUrl) || response.status === 401 || (isHtml && !isServerError)) {
    throw new SessionExpiredError('Session expired');
  }

  // Handle non-2xx responses
  if (!response.ok) {
    let errorData = {};
    try {
      errorData = await response.json();
    } catch (e) {
      // Not JSON, continue with empty object
    }

    // Build error message from various possible fields
    const message =
      errorData.sorry ||
      errorData.error ||
      errorData.message ||
      `Request failed with status ${response.status}`;

    throw new Error(message);
  }

  return response.json();
}

/**
 * Fetch JSON with automatic session expiry detection and error envelope parsing
 * @param {string} url - The URL to fetch
 * @param {Object} options - Fetch options (defaults to {})
 * @returns {Promise<Object>} Parsed JSON response
 * @throws {SessionExpiredError} When session expires (redirect to /login or HTML response)
 * @throws {Error} For other HTTP errors with parsed error message
 */
async function fetchJson(url, options = {}) {
  return readJsonResponse(await fetch(url, options));
}

/**
 * Fetch JSON while preserving the ETag and treating 304 as "no changes".
 * The caller holds the validator in memory; responses stay no-store.
 * An empty 200 for a request that already carried a validator is treated as
 * unchanged, covering intermediaries that normalize 304 to 200.
 * @param {string} url
 * @param {Object} options
 * @returns {Promise<{notModified: boolean, etag: string, data: Object|null}>}
 */
async function fetchJsonWithETag(url, options = {}) {
  const sentEtag = headerValue(options.headers, 'If-None-Match');
  let response = await fetch(url, options);
  let etag = response.headers ? (response.headers.get('ETag') || '') : '';

  if (response.status === 304) {
    // A 304 carrying a different validator did not answer the request we sent.
    // Retry once without the conditional header rather than trusting it.
    if (etag && sentEtag && etag !== sentEtag) {
      response = await fetch(url, withoutHeader(options, 'If-None-Match'));
      etag = response.headers ? (response.headers.get('ETag') || '') : '';
    }
    if (response.status === 304) {
      if (etag && sentEtag && etag !== sentEtag) {
        throw new Error('unexpected 304 response');
      }
      return { notModified: true, etag: etag || sentEtag, data: null };
    }
  }

  if (response.ok && sentEtag && response.status !== 204) {
    const bodyText = await peekResponseText(response);
    if (bodyText === '') {
      return { notModified: true, etag: sentEtag, data: null };
    }
  }

  return { notModified: false, etag, data: await readJsonResponse(response) };
}

function headerValue(headers, name) {
  if (!headers) return '';
  if (typeof headers.get === 'function') return headers.get(name) || '';
  const key = Object.keys(headers).find(
    (candidate) => candidate.toLowerCase() === name.toLowerCase()
  );
  return key ? String(headers[key]) : '';
}

function withoutHeader(options, name) {
  const headers = options.headers;
  if (!headers) return options;
  if (typeof Headers !== 'undefined' && headers instanceof Headers) {
    const copy = new Headers(headers);
    copy.delete(name);
    return { ...options, headers: copy };
  }
  const copy = {};
  for (const [key, value] of Object.entries(headers)) {
    if (key.toLowerCase() !== name.toLowerCase()) copy[key] = value;
  }
  return { ...options, headers: copy };
}

async function peekResponseText(response) {
  if (typeof response.clone !== 'function') return null;
  try {
    return await response.clone().text();
  } catch {
    return null;
  }
}

// Export for browser usage (via script tag)
if (typeof globalThis !== "undefined") {
  if (!globalThis.NWKidsApi) {
    globalThis.NWKidsApi = {};
  }
  globalThis.NWKidsApi.fetchJson = fetchJson;
  globalThis.fetchJson = fetchJson;
  globalThis.NWKidsApi.fetchJsonWithETag = fetchJsonWithETag;
  globalThis.fetchJsonWithETag = fetchJsonWithETag;
  globalThis.NWKidsApi.SessionExpiredError = SessionExpiredError;
  globalThis.SessionExpiredError = SessionExpiredError;
}

if (typeof window !== "undefined") {
  window.NWKidsApi = globalThis.NWKidsApi;
  window.fetchJson = globalThis.NWKidsApi.fetchJson;
  window.fetchJsonWithETag = globalThis.NWKidsApi.fetchJsonWithETag;
  window.SessionExpiredError = globalThis.NWKidsApi.SessionExpiredError;
}

// Export for Node.js/test usage
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    fetchJson,
    fetchJsonWithETag,
    SessionExpiredError
  };
}