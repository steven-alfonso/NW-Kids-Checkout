import { fileURLToPath } from 'node:url';
import { resolve } from 'node:path';
import preact from '@preact/preset-vite';
import { defineConfig } from 'vite';

const root = fileURLToPath(new URL('.', import.meta.url));
const outDir = resolve(root, 'internal/web/static/dist/preact');

export default defineConfig({
  plugins: [preact()],
  build: {
    target: 'es2020',
    outDir,
    emptyOutDir: true,
    copyPublicDir: false,
    cssCodeSplit: false,
    modulePreload: false,
    license: { fileName: 'THIRD_PARTY_NOTICES.md' },
    rollupOptions: {
      input: resolve(root, 'internal/web/src/checkouts-preact/main.jsx'),
      output: {
        format: 'iife',
        entryFileNames: 'checkouts.js',
        assetFileNames: 'checkouts.css'
      }
    }
  }
});
