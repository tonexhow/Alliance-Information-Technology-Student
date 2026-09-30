import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react()],
  publicDir: 'public',
  server: {
    host: '127.0.0.1',
    port: 3014,
    strictPort: true,
    allowedHosts: ['aits.jpano.dev', 'localhost', '127.0.0.1'],
    proxy: {
      '/api': 'http://127.0.0.1:3013',
      '/library': 'http://127.0.0.1:3013',
      // Local 127.0.0.1 clients connect straight to :3013 (see websocket.ts),
      // while Cloudflare/dev-host traffic still needs a same-origin WS bridge.
      '/ws': { target: 'http://127.0.0.1:3013', ws: true, changeOrigin: true },
    },
  },
})
