import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The public landing page, served by captain at /. API calls proxied in dev.
export default defineConfig({
  plugins: [react()],
  base: '/',
  server: { port: 5175, proxy: { '/api': 'http://127.0.0.1:8080' } },
  build: { outDir: 'dist', emptyOutDir: true },
})
