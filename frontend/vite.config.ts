import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The checkout (8081) and underwriting (8082) services share the same `/api`
// prefix, so the dev server proxies two distinct front-end prefixes and strips
// them. This keeps the browser same-origin (no CORS setup needed).
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/checkout': {
        target: 'http://localhost:8081',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/checkout/, ''),
      },
      '/underwriting': {
        target: 'http://localhost:8082',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/underwriting/, ''),
      },
    },
  },
});
