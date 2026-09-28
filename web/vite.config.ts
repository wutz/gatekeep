import { defineConfig } from 'vite'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import viteReact from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Built as a static SPA (served by gatekeepd from web/dist/client).
// In dev, API/MCP calls are proxied to a local gatekeepd.
export default defineConfig({
  server: {
    proxy: {
      '/api': { target: 'http://127.0.0.1:8740', changeOrigin: true },
      '/mcp': { target: 'http://127.0.0.1:8740', changeOrigin: true },
    },
  },
  plugins: [
    tailwindcss(),
    tanstackStart({ spa: { enabled: true } }),
    viteReact(),
  ],
})
