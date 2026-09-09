import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { resolve } from 'node:path';

export default defineConfig(() => ({
  base: '/',
  plugins: [svelte()],
  resolve: {
    alias: {
      $lib: resolve(__dirname, 'src/lib'),
      $components: resolve(__dirname, 'src/components'),
      $stores: resolve(__dirname, 'src/stores'),
      $tabs: resolve(__dirname, 'src/tabs')
    },
    conditions: ['browser']
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    assetsDir: 'assets',
    manifest: true,
    sourcemap: false,
    target: 'es2022',
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]'
      }
    }
  },
  server: {
    port: 5173,
    proxy: {
      // secure: false because the panel's certificate is self-signed until a
      // domain is attached, and the dev proxy has no reason to be stricter
      // than the browser the developer already clicked through.
      '/api': { target: 'https://localhost:7073', changeOrigin: true, ws: true, secure: false },
      '/icons': { target: 'https://localhost:7073', secure: false },
      '/manifest.webmanifest': { target: 'https://localhost:7073', secure: false },
      '/sw.js': { target: 'https://localhost:7073', secure: false },
      '/offline.html': { target: 'https://localhost:7073', secure: false }
    }
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
    // Several suites import the module under test inside the first `it`, because
    // they reset the module registry between cases. That first import compiles
    // the module graph, and vitest charges the whole compile to that one test's
    // timeout: on a cold or busy machine the first test in a file fails at five
    // seconds while every later one in the same file passes in milliseconds.
    // Nothing here is slow on purpose, so the ceiling is set where a real hang
    // still trips it.
    testTimeout: 20000
  }
}));
