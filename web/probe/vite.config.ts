import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The public probe page. Relative base so captain can mount it under any
// path (/status) or on a dedicated host.
export default defineConfig({
  plugins: [react()],
  base: './',
  server: { port: 5176, proxy: { '/api': 'http://127.0.0.1:8080' } },
  build: { outDir: 'dist', emptyOutDir: true },
})
