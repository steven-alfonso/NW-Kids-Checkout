import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// One fixed-name bundle, committed under internal/web/static/dist/svelte so the
// Docker build needs no Node stage. `?v=dev` in the page is rewritten to a
// content hash by cmd/assets in production.
export default defineConfig({
    plugins: [svelte()],
    build: {
        outDir: 'internal/web/static/dist/svelte',
        emptyOutDir: true,
        copyPublicDir: false,
        target: 'es2020',
        rollupOptions: {
            input: 'internal/web/src/checkouts-svelte/main.js',
            output: {
                format: 'iife',
                entryFileNames: 'checkouts.js',
                assetFileNames: 'checkouts.[ext]',
                inlineDynamicImports: true
            }
        }
    }
});
