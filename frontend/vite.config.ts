import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

// In development the Vue dev server proxies the API to the Go binary, so the
// same origin serves both and no CORS setup is ever needed.
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://127.0.0.1:8787', changeOrigin: true }
    }
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 900
  }
});
