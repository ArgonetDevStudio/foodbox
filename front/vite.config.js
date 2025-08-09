import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// https://vite.dev/config/
export default defineConfig({
  plugins: [svelte()],
  server: {
    host: true,
    proxy: {
      '/api': {
        target: 'http://foodbox.o-r.kr',
        changeOrigin: true,
        secure: true,
      }
    }
  },
  build: {
    outDir: 'dist'
  }
})
