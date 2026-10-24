import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// The build lands inside the Go module so it is embedded into the server binary.
const outDir = fileURLToPath(new URL('../server/internal/web/dist', import.meta.url))

export default defineConfig({
  plugins: [
    react(),
    {
      // emptyOutDir wipes the placeholder that keeps go:embed valid; put it back.
      name: 'keep-gitkeep',
      closeBundle() {
        writeFileSync(`${outDir}/.gitkeep`, '')
      },
    },
  ],
  // The lazily loaded player chunk is ~600 KB because of hls.js.
  build: { outDir, emptyOutDir: true, chunkSizeWarningLimit: 700 },
  server: {
    port: 5173,
    proxy: { '/api': 'http://localhost:8484' },
  },
})
