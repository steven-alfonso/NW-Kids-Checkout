import { fileURLToPath, URL } from 'node:url';
import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

// One fixed-name bundle per page implementation. The Go binary embeds
// internal/web/static, so the output is committed and no Node build stage is
// needed in the Docker image. `emptyOutDir` keeps a deleted source module from
// lingering in the shipped directory.
export default defineConfig({
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
    target: 'es2020',
    rollupOptions: {
      input: fileURLToPath(
        new URL('./internal/web/src/checkouts-vue/main.js', import.meta.url)
      ),
      output: {
        entryFileNames: 'checkouts.js',
        chunkFileNames: 'checkouts-[name].js',
        assetFileNames: 'checkouts[extname]',
        inlineDynamicImports: true
      }
    }
  }
});
