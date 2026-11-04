// Dev-only asset (see internal/web/dev-assets/README.md for how this is
// served and how to add new dev tools).
//
// Dev helper: loadPreviewData() seeds demo checkouts so you can visually
// validate pill colors and the confirm checkbox without waiting for real time.
// Call from the browser console on the checkouts page: loadPreviewData()
// Use the ?debug param for extra console logging while previewing.
//
// The Svelte page owns its state, so this helper delegates through the small
// handle the app exposes on window.
function loadPreviewData() {
    if (
        window.__checkoutsPreview &&
        typeof window.__checkoutsPreview.loadPreviewData === 'function'
    ) {
        window.__checkoutsPreview.loadPreviewData();
        return;
    }
    console.warn('Checkouts preview handle is not ready yet.');
}
