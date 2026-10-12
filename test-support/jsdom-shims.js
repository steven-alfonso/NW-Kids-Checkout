// Shared jsdom shims for the JS test suite.
//
// This file lives outside internal/web/static on purpose. That directory is
// embedded wholesale by //go:embed * in internal/web/static/static.go, so
// anything placed there ends up in the production binary. Keeping test support
// at the repository root keeps it out of the artifact.
//
// The shims are not applied through vitest's setupFiles. These tests do not use
// vitest's jsdom environment at all: they read a page script from disk and eval
// it into a JSDOM instance each test constructs, because the page scripts are
// plain scripts that expect globals rather than modules. There is therefore no
// global environment to patch, and each loadWindow has to call stubJsdomGaps
// against its own instance.

/**
 * Replace the jsdom APIs that report "Not implemented" through their virtual
 * console when called.
 *
 * jsdom defines these as functions but leaves them unimplemented, so calling one
 * prints an error to stderr and vitest attributes it to whichever test happened to
 * be running. The application code already guards window.scrollTo in a try/catch
 * for exactly this reason -- but the console report happens inside jsdom before
 * the throw reaches that catch, so the guard cannot suppress the output.
 *
 * These are environment gaps, not application bugs. Nothing in production code
 * should change to accommodate them, and no test asserts on scrollTo, so
 * stubbing it cannot hide a behavioural regression.
 *
 * Call this on the JSDOM instance before evaluating the script under test, so the
 * script captures the stub rather than jsdom's own.
 *
 * @param {import('jsdom').JSDOM} dom
 */
export function stubJsdomGaps(dom) {
    dom.window.scrollTo = () => {};
}
