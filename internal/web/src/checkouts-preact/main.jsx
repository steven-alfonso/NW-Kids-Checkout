import { render } from 'preact';
import { App } from './App.jsx';

const root = document.getElementById('checkouts-app');
if (root) {
  try {
    // Portal hosts have static fallbacks for the bundle-failure case; clear
    // them before the app portals its interactive controls into place.
    document.getElementById('current-time')?.replaceChildren();
    document.getElementById('search-toggle-host')?.replaceChildren();
    render(<App />, root);
  } catch (error) {
    console.error('Could not start the checkouts page:', error);
    root.innerHTML =
      '<div class="text-center text-gray-600 py-8">Unable to load checkouts. Refresh the page.</div>';
  }
}
