import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      '/otel-ws': {
        target: 'http://localhost:8085',
        ws: true,
      },
      '/ws': {
        target: 'http://localhost:8085',
        ws: true,
      },
    },
  },
});

