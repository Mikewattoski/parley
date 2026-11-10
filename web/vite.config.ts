import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // The browser talks to one origin (5173); Vite forwards API and WebSocket
    // traffic to the Go server, so no CORS configuration is needed.
    proxy: {
      '/api': 'http://127.0.0.1:8081',
      '/ws': { target: 'http://127.0.0.1:8081', ws: true },
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: false,
  },
});
