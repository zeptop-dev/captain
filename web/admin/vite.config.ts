import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Served by captain at /admin/; in dev, API calls are proxied to the Go server.
export default defineConfig({
  plugins: [react()],
  base: '/admin/',
  server: {
    port: 5173,
    proxy: { '/api': 'http://127.0.0.1:8080', '/sub': 'http://127.0.0.1:8080' },
  },
  build: { outDir: 'dist', emptyOutDir: true },
})
