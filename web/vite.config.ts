import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// Dev-server proxy target: the Go server's default bind address
// (internal/config's default port is 8080). Overridable so a
// contributor running `tickets server --port 9090` doesn't have to
// edit this file.
const apiProxyTarget = process.env.TICKETS_DEV_API_URL ?? 'http://127.0.0.1:8080'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    // Vite's default behavior empties outDir before every build — and
    // deletes dist/.gitkeep along with it, since that file isn't part
    // of Vite's own output and Vite doesn't know to spare it. That
    // defeats .gitkeep's entire purpose (a tracked placeholder so
    // go:embed/go build succeed on a checkout that never ran npm,
    // ADR 0010): a real local build would silently leave the tracked
    // file deleted, showing up as an uncommitted deletion in `git
    // status` after every `task web:build`. Disabling emptyOutDir
    // keeps .gitkeep intact across rebuilds; the cost is old
    // content-hashed asset files (assets/index-<hash>.js) from a
    // previous build accumulating in a long-lived local dist/ instead
    // of being swept — harmless (gitignored, never committed, and a
    // fresh checkout starts empty regardless) next to what breaking
    // .gitkeep would cost.
    emptyOutDir: false,
  },
  server: {
    // Proxied, not cross-origin fetch: the session cookie is
    // SameSite=Lax (ADR 0004), so a cross-origin request from the Vite
    // dev server's own origin to the Go server would silently drop it
    // and the CSRF flow would appear broken for no obvious reason.
    // Proxying makes dev-mode traffic same-origin, exactly like the
    // production build (served directly by internal/httpapi) already is.
    proxy: {
      '/api/v1': { target: apiProxyTarget, changeOrigin: true },
      '/healthz': { target: apiProxyTarget, changeOrigin: true },
    },
  },
  test: {
    // jsdom: Milestone 2 adds the first real React components (auth
    // shell, read-only views) worth testing with React Testing
    // Library. No `globals: true` — test files import
    // describe/it/expect from 'vitest' explicitly rather than relying
    // on injected globals.
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
  },
})
