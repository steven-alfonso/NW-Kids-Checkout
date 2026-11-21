import { fileURLToPath, URL } from 'node:url';
import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

// One bundle per page implementation, keyed by entry name. The Go binary embeds
// internal/web/static, so the output is committed and no Node build stage is
// needed in the Docker image. `emptyOutDir` keeps a deleted source module from
// lingering in the shipped directory. Named entries and `[name]` output keep
// checkouts.js where the page links it even when a second page is added;
// hashed chunk names stay cache-safe under the immutable /static cache header.
// The one shared stylesheet keeps its fixed name because the page links it.
export default defineConfig({
  base: '/static/dist/vue/',
  plugins: [vue()],
  resolve: {
    alias: {
      vue: 'vue/dist/vue.runtime.esm-bundler.js'
    }
  },
  define: {
    __VUE_OPTIONS_API__: 'false',
    __VUE_PROD_DEVTOOLS__: 'false',
    __VUE_PROD_HYDRATION_MISMATCH_DETAILS__: 'false'
  },
  build: {
    outDir: 'internal/web/static/dist/vue',
    emptyOutDir: true,
    cssCodeSplit: false,
    minify: 'oxc',
    license: { fileName: 'THIRD_PARTY_NOTICES.md' },
    target: 'es2020',
    rollupOptions: {
      input: {
        checkouts: fileURLToPath(
          new URL('./internal/web/src/checkouts-vue/main.js', import.meta.url)
        )
      },
      output: {
        entryFileNames: '[name].js',
        chunkFileNames: '[name]-[hash].js',
        // cssCodeSplit is off, so every entry shares one stylesheet. Its name
        // is fixed because the page links /static/dist/vue/checkouts.css.
        assetFileNames: 'checkouts[extname]'
      }
    }
  }
});
