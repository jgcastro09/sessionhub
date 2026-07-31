import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// During `npm run dev` the panel needs a running SessionHub to talk to.
// Point SESSIONHUB_WEB_TARGET at it (defaults to the panel's own default
// port, see internal/app/webpanel.go's DefaultWebPanelPort) so requests to
// /api/* proxy through without CORS/cookie headaches during development.
const target = process.env.SESSIONHUB_WEB_TARGET ?? 'http://127.0.0.1:8420'

export default defineConfig({
  plugins: [react()],
  server: {
    host: true,
    proxy: {
      '/api': { target, changeOrigin: true },
    },
  },
  build: {
    // Built straight into the Go package that go:embed's it — see
    // internal/webserver/assets.go — so `npm run build` is the only step
    // needed before `go build ./cmd/sessionhub` picks up a fresh UI.
    outDir: '../internal/webserver/dist',
    emptyOutDir: true,
  },
})
