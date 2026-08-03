import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { federation } from '@module-federation/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    react(),
    // Module Federation: expose vocat as a remote the portal can mount in-process.
    federation({
      name: 'vocat',
      filename: 'remoteEntry.js',
      exposes: {
        './App': './src/embed',
      },
      shared: ['react', 'react-dom'],
    }),
  ],
  server: {
    port: 5174,
    origin: 'http://localhost:5174',
  },
})
