import { createApp } from 'vue';
import App from './App.vue';

const root = document.getElementById('app');
if (root) {
  try {
    // Portal hosts have static fallbacks for the bundle-failure case; clear
    // them before Vue teleports the interactive controls into place.
    document.getElementById('current-time')?.replaceChildren();
    document.getElementById('search-toggle-slot')?.replaceChildren();
    createApp(App).mount(root);
  } catch (error) {
    console.error('Could not start the checkouts page:', error);
    root.innerHTML =
      '<div class="text-center text-gray-600 py-8">Unable to load checkouts. Refresh the page.</div>';
  }
}
