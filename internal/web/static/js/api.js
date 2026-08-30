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
 * Fetch JSON with automatic session expiry detection and error envelope parsing
 * @param {string} url - The URL to fetch
 * @param {Object} options - Fetch options (defaults to {})
 * @returns {Promise<Object>} Parsed JSON response
 * @throws {SessionExpiredError} When session expires (redirect to /login or HTML response)
 * @throws {Error} For other HTTP errors with parsed error message
 */
async function fetchJson(url, options = {}) {
  const response = await fetch(url, options);

  const responseUrl = response.url || '';
  const contentType = response.headers ? (response.headers.get('content-type') || '') : '';
  const isHtml = contentType.includes('text/html');

  // Check for session expiry via redirect to login or HTML response
  if (response.redirected || responseUrl.endsWith('/login') || isHtml) {
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

  // Parse and return JSON response
  return response.json();
}

// Export for browser usage (via script tag)
if (typeof globalThis !== "undefined") {
  if (!globalThis.NWKidsApi) {
    globalThis.NWKidsApi = {};
  }
  globalThis.NWKidsApi.fetchJson = fetchJson;
  globalThis.fetchJson = fetchJson;
  globalThis.NWKidsApi.SessionExpiredError = SessionExpiredError;
  globalThis.SessionExpiredError = SessionExpiredError;
}

if (typeof window !== "undefined") {
  window.NWKidsApi = globalThis.NWKidsApi;
  window.fetchJson = globalThis.NWKidsApi.fetchJson;
  window.SessionExpiredError = globalThis.NWKidsApi.SessionExpiredError;
}

// Export for Node.js/test usage
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    fetchJson,
    SessionExpiredError
  };
}