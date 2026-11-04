// Dev-only asset (see internal/web/dev-assets/README.md for how this is
// served and how to add new dev tools).
//
// Dev helper: loadPreviewData() seeds demo checkouts so you can visually
// validate pill colors and the confirm checkbox without waiting for real time.
// Call from the browser console on the checkouts page: loadPreviewData().
//
// The page owns its state; this helper only signals it. The page dynamically
// imports the dev-only fixture and renders it.
function loadPreviewData() {
    window.dispatchEvent(new CustomEvent('checkouts-preview'));
}
