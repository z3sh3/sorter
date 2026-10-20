import path from "node:path"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"

// Rolldown module-id matcher for recharts and its exclusive dependencies.
// Anchoring each segment with a trailing slash avoids matching the flattened
// pnpm store prefixes such as `.pnpm/recharts@3.10.1...`.
const RECHARTS_MODULES =
  /[\\/](recharts|victory-vendor|react-smooth|decimal\.js-light|es-toolkit|eventemitter3|immer|reselect|react-redux|@reduxjs[\\/]toolkit|use-sync-external-store)[\\/]/

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  server: {
    proxy: {
      // Forward API calls to the Go backend during development.
      "/api": "http://localhost:8080",
    },
  },
  build: {
    // Split the charting stack into its own chunk so it is cached separately
    // from application code and does not bloat the entry bundle.
    rolldownOptions: {
      output: {
        codeSplitting: {
          groups: [{ name: "recharts", test: RECHARTS_MODULES, priority: 10 }],
        },
      },
    },
  },
})
