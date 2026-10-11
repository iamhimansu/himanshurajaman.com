import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// The Vite project root is this folder (web/), so index.html lives beside this
// config and the build writes to web/dist, which web/embed.go embeds with
// `//go:embed all:dist`. base: '/' because the app is served from the site root.
//
// In dev, /api and /images are proxied to the Go server (default :8080) so the
// frontend uses the same relative URLs it will use in production (same origin).
export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: '/',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/images': 'http://localhost:8080',
    },
  },
})
