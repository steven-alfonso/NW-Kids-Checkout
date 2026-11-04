import { mount } from 'svelte';
import App from './App.svelte';

const target = document.getElementById('checkouts-app');

if (target && !window.__checkoutsInitialized) {
    window.__checkoutsInitialized = true;

    // The Go handler replaces the placeholder inside this hidden source node
    // with auth-aware links. Hand the rendered HTML to Svelte, then drop the
    // source so the menu ids exist exactly once in the document.
    const menuSource = document.getElementById('checkouts-menu-source');
    const menuHtml = menuSource ? menuSource.innerHTML : '';
    menuSource?.remove();

    mount(App, { target, props: { menuHtml } });
}
