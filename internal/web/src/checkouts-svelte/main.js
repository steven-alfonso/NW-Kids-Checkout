import { mount } from 'svelte';
import App from './App.svelte';
import { readMenuHtml } from './lib/menu.js';

const target = document.getElementById('checkouts-app');

if (target && !window.__checkoutsInitialized) {
    window.__checkoutsInitialized = true;

    const menuHtml = readMenuHtml(document);
    target.replaceChildren();
    mount(App, { target, props: { menuHtml } });
}
